package service

// 移动云盘（139 / 和彩云）登录与令牌管理。
//
// 鉴权模型（参考 OpenList drivers/139，文档 https://doc.oplist.org/guide/drivers/139）：
//   - Authorization：Basic 后面的值，内容为 base64("pc:<account>:<authToken>")，
//     authToken 形如 a|b|c|<过期毫秒时间戳>|...，可用于所有个人云接口。
//   - 新的个人盘（personal_new）走 /file/list 等接口，需要 Basic 鉴权 + Mcloud-Sign 签名。
//   - 账号密码回退登录三步：
//       1) POST mail.10086.cn/Login/Login.ashx（密码 SHA1("fetion.com.cn:<pwd>")）→ sid
//       2) POST smsrebuild1.mail.10086.cn/setting/s?func=umc:getArtifact → artifact（需 RMKEY cookie）
//       3) POST user-njs.yun.139.com/user/thirdlogin（AES-CBC 加密请求）→ authToken/account/userDomainId
//   - 令牌临近过期（<15 天）时用 authTokenRefresh.do 续期，失败则回退账号密码登录。

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"tgto/internal/logger"
)

const (
	y139Key1 = "73634235495062495331515373756c734e7253306c673d3d" // 第一层 AES 密钥（hex）
	y139Key2 = "7150714477323633586746674c337538"                 // 第二层 AES 密钥（hex）

	y139MailLoginURL  = "https://mail.10086.cn/Login/Login.ashx"
	y139MailSMSURL    = "https://mail.10086.cn/s"
	y139ArtifactURL   = "https://smsrebuild1.mail.10086.cn/setting/s"
	y139ThirdLoginURL = "https://user-njs.yun.139.com/user/thirdlogin"
	y139RefreshURL    = "https://aas.caiyun.feixin.10086.cn:443/tellin/authTokenRefresh.do"
	y139RouteURL      = "https://user-njs.yun.139.com/user/route/qryRoutePolicy"

	y139BrowserUA = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/141.0.0.0 Safari/537.36 Edg/141.0.0.0"
	y139KeepAlive = 12 * time.Hour
)

// 便于测试替换（线上恒为官方地址）。
var (
	y139MailLoginBase  = y139MailLoginURL
	y139ArtifactBase   = y139ArtifactURL
	y139ThirdLoginBase = y139ThirdLoginURL
	y139RefreshBase    = y139RefreshURL
	y139RouteBase      = y139RouteURL
)

// y139SMSState 一次短信验证的中间态（进程内存，避免把验证码落盘）。
type y139SMSState struct {
	RiskCode string
	Username string
	Cookies  string
}

var (
	y139SMSMu   sync.Mutex
	y139SMSPre  *y139SMSState
	y139MailSMS = y139MailSMSURL
	y139MailPub = "MIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAnsOHTFtwW5rq/8gGhPlM5Z3RPdeN/d+FYIHb5JmcfBOCozXuT8c+0anvxtkjzghixwNlnmBuhN8OYfS789YuH/qReQbHC7OdlisLildNWPHRNUYcPa0W3lXSG3+81CXK7FDXPvXo5ubw2GqVbIsccMarI1dyfXdi4ITiCXvmM9wYBdUs9yXtoorhlpyYUI2GV8HNsQjWK9P5QZHT3ox5Qy+mjRmvv6RUFJLPOMkOS/pGZ0DwC1ypFZBxstW0/ftVupdOmGWvW7J2/e3dq3A/UvIkC4YUY/diL1wighJx1G9MiRROISjNMvNyUDSTqPJy516+l3sgHEbc067QIJx2NQIDAQAB"
)

// y139RiskScene 10086 邮箱登录风控码 -> 短信验证场景（OpenList 同款映射）。
func y139RiskScene(code string) (int, bool) {
	switch code {
	case "PML401010062":
		return 2, true
	case "MW0016":
		return 4, true
	case "S025", "S035":
		return 1, true
	}
	return 0, false
}

func y139RiskCodeOf(location string) string {
	u, err := url.Parse(location)
	if err != nil {
		return ""
	}
	return u.Query().Get("ec")
}

// y139Auth 持久化到 DataDir/yun139_auth.json。
type y139Auth struct {
	Authorization string `json:"authorization"`
	Account       string `json:"account"`
	UserDomainID  string `json:"user_domain_id"`
	ExpiresAt     int64  `json:"expires_at"` // 毫秒时间戳，0 表示未知
	UpdatedAt     int64  `json:"updated_at"`
}

// ---------------- 状态 / 持久化 ----------------

func (s *Services) y139AuthPath() string {
	return filepath.Join(s.DataDir, "yun139_auth.json")
}

func (s *Services) loadY139Auth() (*y139Auth, error) {
	raw, err := os.ReadFile(s.y139AuthPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	auth := &y139Auth{}
	if err := json.Unmarshal(raw, auth); err != nil {
		return nil, err
	}
	return auth, nil
}

func (s *Services) saveY139Auth(auth *y139Auth) error {
	if err := os.MkdirAll(s.DataDir, 0o755); err != nil {
		return err
	}
	auth.UpdatedAt = time.Now().Unix()
	raw, err := json.MarshalIndent(auth, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.y139AuthPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.y139AuthPath())
}

// Yun139Status 返回登录状态（供 /api/state 与前端面板使用）。
func (s *Services) Yun139Status() map[string]any {
	cfg := s.GetConfig(Drive139)
	username := toString(cfg["username"])
	hasPwd := toString(cfg["password"]) != ""
	hasCookies := toString(cfg["mail_cookies"]) != ""
	hasAuthField := toString(cfg["authorization"]) != ""

	auth, err := s.loadY139Auth()
	status := map[string]any{
		"logged_in":    false,
		"status":       "未登录：填写账号密码（可附邮箱 Cookie）后点击「登录」",
		"account":      username,
		"login_mode":   "",
		"updated_at":   0,
		"expires_at":   0,
		"has_password": hasPwd,
		"has_cookies":  hasCookies,
	}
	if err != nil {
		status["status"] = "读取本地令牌失败: " + err.Error()
		return status
	}
	if auth == nil || auth.Authorization == "" {
		if hasAuthField {
			status["status"] = "已填写授权令牌，但尚未校验（点击「校验令牌」）"
		}
		return status
	}
	status["logged_in"] = true
	status["account"] = orDefault(auth.Account, username)
	status["updated_at"] = auth.UpdatedAt
	status["expires_at"] = auth.ExpiresAt
	if auth.ExpiresAt > 0 && time.Now().UnixMilli() > auth.ExpiresAt {
		status["logged_in"] = false
		status["status"] = "授权令牌已过期，请重新登录"
		return status
	}
	status["login_mode"] = "authorization"
	status["status"] = "已登录（授权令牌有效）"
	if auth.ExpiresAt > 0 {
		days := (auth.ExpiresAt - time.Now().UnixMilli()) / (1000 * 60 * 60 * 24)
		status["status"] = fmt.Sprintf("已登录（令牌剩余约 %d 天）", days)
	}
	return status
}

// Yun139Clear 清除本地令牌。
func (s *Services) Yun139Clear() (map[string]any, error) {
	if err := os.Remove(s.y139AuthPath()); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 已清除本地登录令牌")
	return map[string]any{"logged_in": false, "status": "已清除移动云盘登录信息"}, nil
}

// ---------------- 登录 ----------------

// Yun139Login 账号密码回退登录：Cookie(可选) + 账号 + 密码 → Authorization。
func (s *Services) Yun139Login(ctx context.Context) (map[string]any, error) {
	cfg := s.GetConfig(Drive139)
	username := strings.TrimSpace(toString(cfg["username"]))
	password := toString(cfg["password"])
	cookies := strings.TrimSpace(toString(cfg["mail_cookies"]))

	// 1) 已手填授权令牌：直接校验
	if tok := strings.TrimSpace(toString(cfg["authorization"])); tok != "" {
		if _, err := s.y139VerifyAuthorization(ctx, tok); err == nil {
			return s.Yun139Status(), nil
		}
		s.Log.Warn(logger.ModCloud139, "[移动云盘] 配置里的授权令牌无效，尝试其它登录方式")
	}

	// 2) 邮箱 Cookie 快速登录：Cookie 里同时有 Os_SSo_Sid 与 RMKEY 时，
	//    跳过邮箱密码登录，直接用 sid 走第 2/3 步（OpenList 同款策略，规避 10086 登录风控）。
	if sid := y139CookieValue(cookies, "Os_SSo_Sid"); sid != "" && y139CookieValue(cookies, "RMKEY") != "" {
		if _, err := s.y139LoginBySID(ctx, username, sid, cookies); err == nil {
			return s.Yun139Status(), nil
		} else {
			s.Log.Warn(logger.ModCloud139, "[移动云盘] 邮箱 Cookie 快速登录失败：%v", err)
			if username == "" || password == "" {
				return nil, fmt.Errorf("邮箱 Cookie 快速登录失败：%v；可补填账号密码后重试", err)
			}
		}
	}

	if username == "" || password == "" {
		return nil, fmt.Errorf("没有可用的登录凭据：请填写「登录账号 + 登录密码」（建议同时粘贴邮箱 Cookie），或粘贴含 Os_SSo_Sid 与 RMKEY 的邮箱 Cookie")
	}

	// 3) 账号密码回退登录
	sid, mergedCookies, err := s.y139Step1(ctx, username, password, cookies)
	var riskErr *y139RiskError
	if errors.As(err, &riskErr) {
		// 触发风控：自动发送短信验证码，让用户输码后重新点登录
		if serr := s.y139SendSMS(ctx); serr != nil {
			return nil, fmt.Errorf("登录需要短信验证（ec=%s），但发送验证码失败: %v", riskErr.RiskCode, serr)
		}
		return map[string]any{
			"sms_required": true,
			"risk_code":    riskErr.RiskCode,
			"logged_in":    false,
			"status":       fmt.Sprintf("已向 %s 发送短信验证码（风控 ec=%s），请输入验证码后再次点击「账号密码登录」", username, riskErr.RiskCode),
		}, nil
	}
	if err != nil {
		return nil, err
	}
	return s.y139FinishLogin(ctx, username, sid, mergedCookies)
}

// y139RiskError 邮箱登录触发风控（可短信验证）的哨兵错误。
type y139RiskError struct {
	RiskCode string
	Scene    int
}

func (e *y139RiskError) Error() string {
	return fmt.Sprintf("邮箱登录触发风控（ec=%s），需要短信验证", e.RiskCode)
}

// Yun139LoginSMS 用短信验证码完成登录（配合 Yun139Login 自动发送的验证码）。
func (s *Services) Yun139LoginSMS(ctx context.Context, code string) (map[string]any, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("请输入短信验证码")
	}
	y139SMSMu.Lock()
	pre := y139SMSPre
	y139SMSMu.Unlock()
	if pre == nil {
		return nil, fmt.Errorf("尚未触发短信验证：请先点「账号密码登录」，系统会自动发送验证码")
	}
	sid, merged, err := s.y139VerifySMS(ctx, pre, code)
	if err != nil {
		return nil, err
	}
	y139SMSMu.Lock()
	y139SMSPre = nil
	y139SMSMu.Unlock()
	if merged != "" {
		_, _ = s.SaveConfig(Drive139, map[string]any{"mail_cookies": merged})
	}
	return s.y139FinishLogin(ctx, pre.Username, sid, merged)
}

// Yun139SMSSend 重发短信验证码（没有进行中的验证时，先触发一次登录拿风控码）。
func (s *Services) Yun139SMSSend(ctx context.Context) (map[string]any, error) {
	cfg := s.GetConfig(Drive139)
	username := strings.TrimSpace(toString(cfg["username"]))
	password := toString(cfg["password"])
	if username == "" || password == "" {
		return nil, fmt.Errorf("请先填写「登录账号」与「登录密码」")
	}
	y139SMSMu.Lock()
	pre := y139SMSPre
	y139SMSMu.Unlock()
	if pre == nil || pre.Username != username {
		_, merged, err := s.y139Step1(ctx, username, password, strings.TrimSpace(toString(cfg["mail_cookies"])))
		var riskErr *y139RiskError
		if !errors.As(err, &riskErr) {
			if err == nil {
				return nil, fmt.Errorf("本次登录未触发短信验证，直接点「账号密码登录」即可")
			}
			return nil, err
		}
		_ = merged
	}
	if err := s.y139SendSMS(ctx); err != nil {
		return nil, err
	}
	return map[string]any{"sms_required": true, "status": "短信验证码已发送，请查收后输入并登录"}, nil
}

// y139FinishLogin sid -> artifact -> Authorization -> 落盘。
func (s *Services) y139FinishLogin(ctx context.Context, username, sid, cookies string) (map[string]any, error) {
	artifact, err := s.y139Step2(ctx, sid, cookies)
	if err != nil {
		return nil, err
	}
	auth, err := s.y139Step3(ctx, username, artifact)
	if err != nil {
		return nil, err
	}
	if err := s.saveY139Auth(auth); err != nil {
		return nil, err
	}
	patch := map[string]any{}
	if auth.Account != "" {
		patch["account"] = auth.Account
	}
	if cookies != "" {
		patch["mail_cookies"] = cookies
	}
	if len(patch) > 0 {
		_, _ = s.SaveConfig(Drive139, patch)
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 账号密码登录成功，账号=%s", auth.Account)
	return s.Yun139Status(), nil
}

// y139SendSMS 调 login:sendSmsCodeByScene 发送短信验证码（使用 step1 存下的风控态）。
func (s *Services) y139SendSMS(ctx context.Context) error {
	y139SMSMu.Lock()
	pre := y139SMSPre
	y139SMSMu.Unlock()
	if pre == nil {
		return fmt.Errorf("没有进行中的短信验证")
	}
	scene, ok := y139RiskScene(pre.RiskCode)
	if !ok {
		return fmt.Errorf("风控码 %s 不支持短信验证", pre.RiskCode)
	}
	loginName, err := y139EncryptLoginName(pre.Username)
	if err != nil {
		return err
	}
	fields := [][2]string{
		{"loginName", loginName},
		{"fv", "4"},
		{"clientId", "1003"},
		{"eMode", "1"},
		{"loginFailureUrl", ""},
		{"loginSuccessUrl", ""},
		{"verifyCode", ""},
		{"version", "1.0"},
		{"scene", strconv.Itoa(scene)},
	}
	var b strings.Builder
	b.WriteString("<object>")
	for _, f := range fields {
		b.WriteString(`<string name="` + f[0] + `">` + y139EscapeXML(f[1]) + `</string>`)
	}
	b.WriteString("</object>")

	u := y139MailSMS + "?func=" + url.QueryEscape("login:sendSmsCodeByScene") + "&cguid=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(b.String()))
	if err != nil {
		return err
	}
	req.Header.Set("Cookie", pre.Cookies)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("User-Agent", "okhttp/4.12.0")
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	parsed := y139ParseMailLogin(body)
	if parsed.Code == "S_OK" {
		s.Log.Info(logger.ModCloud139, "[移动云盘] 短信验证码已发送（scene=%d，账号=%s）", scene, pre.Username)
		return nil
	}
	switch parsed.Code {
	case "PML401010021", "PML401010022":
		return fmt.Errorf("需要先完成图形验证码才能发送短信（code=%s），请在浏览器登录一次 mail.10086.cn", parsed.Code)
	case "PML404010001":
		return errors.New("短信验证码发送过于频繁，请稍后再试")
	case "PML401010002":
		return errors.New("139 邮箱拒绝了短信验证参数：该账号可能已开启登录保护或风控升级，请先用浏览器登录一次 mail.10086.cn，再把完整 Cookie 贴到「邮箱 Cookie」后点登录")
	}
	return fmt.Errorf("发送短信验证码失败（code=%s %s）", parsed.Code, parsed.Summary)
}

// y139VerifySMS 用短信验证码完成邮箱登录，返回 sid 与合并后的 Cookie。
func (s *Services) y139VerifySMS(ctx context.Context, pre *y139SMSState, code string) (string, string, error) {
	scene, ok := y139RiskScene(pre.RiskCode)
	if !ok {
		return "", pre.Cookies, fmt.Errorf("风控码 %s 不支持短信验证", pre.RiskCode)
	}
	loginName, err := y139EncryptLoginName(pre.Username)
	if err != nil {
		return "", pre.Cookies, err
	}
	// 短信验证码按「密码」规则参与登录：SHA1("fetion.com.cn:<code>")
	pwdHash := y139SHA1("fetion.com.cn:" + code)
	fields := [][2]string{
		{"clientId", "1003"},
		{"version", "4"},
		{"loginType", "0"},
		{"authType", "2"},
		{"loginName", loginName},
		{"eMode", "1"},
		{"loginPassword", pwdHash},
		{"createAutoLoginSecretKey", "1"},
		{"verifyCode", ""},
		{"verifyAgentId", ""},
		{"reqFrom", "3"},
		{"needWCookie", "1"},
	}
	if pre.RiskCode == "MW0016" {
		fields = append(fields, [2]string{"pwdType", "1"})
	}
	var b strings.Builder
	b.WriteString("<object>")
	for _, f := range fields {
		b.WriteString(`<string name="` + f[0] + `">` + y139EscapeXML(f[1]) + `</string>`)
	}
	b.WriteString("</object>")

	u := y139MailSMS + "?func=" + url.QueryEscape("/login/inlogin.action") + "&cguid=" + strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(b.String()))
	if err != nil {
		return "", pre.Cookies, err
	}
	req.Header.Set("Cookie", pre.Cookies)
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	req.Header.Set("User-Agent", "okhttp/4.12.0")
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return "", pre.Cookies, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	merged := y139MergeCookies(pre.Cookies, resp.Cookies())

	parsed := y139ParseMailLogin(body)
	if parsed.Code != "S_OK" {
		return "", merged, fmt.Errorf("短信验证失败（code=%s %s）", parsed.Code, parsed.Summary)
	}
	sid := ""
	if parsed.LoginSuccessURL != "" {
		if u2, perr := url.Parse(parsed.LoginSuccessURL); perr == nil {
			sid = u2.Query().Get("sid")
		}
	}
	if sid == "" {
		sid = y139CookieValue(merged, "Os_SSo_Sid")
		if sid == "" {
			sid = y139CookieValue(merged, "sid")
		}
	}
	if sid == "" {
		return "", merged, errors.New("短信验证成功但未返回 sid")
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 短信验证通过（scene=%d）", scene)
	return sid, merged, nil
}

// y139EncryptLoginName 用邮箱公钥加密账号（发短信/验证短信接口要求）。
func y139EncryptLoginName(account string) (string, error) {
	der, err := base64.StdEncoding.DecodeString(y139MailPub)
	if err != nil {
		return "", fmt.Errorf("邮箱公钥解码失败（常量损坏）: %w", err)
	}
	pub, err := x509.ParsePKIXPublicKey(der)
	if err != nil {
		return "", fmt.Errorf("邮箱公钥解析失败: %w", err)
	}
	rsaPub, ok := pub.(*rsa.PublicKey)
	if !ok {
		return "", errors.New("邮箱公钥不是 RSA")
	}
	enc, err := rsa.EncryptPKCS1v15(crand.Reader, rsaPub, []byte(account))
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(enc), nil
}

// y139ParseMailLogin 解析邮箱登录接口响应（兼容 JSON 与 JS 变量风格）。
func y139ParseMailLogin(body []byte) struct {
	Code            string
	Summary         string
	LoginSuccessURL string
} {
	out := struct {
		Code            string
		Summary         string
		LoginSuccessURL string
	}{}
	var parsed struct {
		Code    string `json:"code"`
		Summary string `json:"summary"`
		Var     struct {
			LoginSuccessURL string `json:"loginSuccessUrl"`
		} `json:"var"`
	}
	if err := json.Unmarshal(body, &parsed); err == nil && (parsed.Code != "" || parsed.Summary != "" || parsed.Var.LoginSuccessURL != "") {
		out.Code = parsed.Code
		out.Summary = parsed.Summary
		out.LoginSuccessURL = parsed.Var.LoginSuccessURL
		return out
	}
	out.Code = y139RegexString(body, `[\"]?code[\"]?\s*:\s*[\"]([^\"]+)`)
	out.Summary = y139RegexString(body, `[\"]?summary[\"]?\s*:\s*[\"]([^\"]+)`)
	out.LoginSuccessURL = y139RegexString(body, `[\"]?loginSuccessUrl[\"]?\s*:\s*[\"]([^\"]+)`)
	return out
}

func y139RegexString(body []byte, pattern string) string {
	re := regexp.MustCompile(pattern)
	if m := re.FindSubmatch(body); len(m) == 2 {
		return string(m[1])
	}
	return ""
}

// y139LoginBySID 用已有 sid（来自邮箱 Cookie）直接换取 Authorization。
func (s *Services) y139LoginBySID(ctx context.Context, username, sid, cookies string) (*y139Auth, error) {
	artifact, err := s.y139Step2(ctx, sid, cookies)
	if err != nil {
		return nil, err
	}
	auth, err := s.y139Step3(ctx, username, artifact)
	if err != nil {
		return nil, err
	}
	if err := s.saveY139Auth(auth); err != nil {
		return nil, err
	}
	patch := map[string]any{}
	if auth.Account != "" {
		patch["account"] = auth.Account
	}
	if len(patch) > 0 {
		_, _ = s.SaveConfig(Drive139, patch)
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 邮箱 Cookie 快速登录成功，账号=%s", auth.Account)
	return auth, nil
}

// y139Step1 邮箱密码登录，返回 sid 与合并后的 Cookie。
func (s *Services) y139Step1(ctx context.Context, username, password, cookies string) (string, string, error) {
	hashed := y139SHA1("fetion.com.cn:" + password)
	cguid := strconv.FormatInt(time.Now().UnixMilli(), 10)
	form := url.Values{}
	form.Set("UserName", username)
	form.Set("passOld", "")
	form.Set("auto", "on")
	form.Set("Password", hashed)
	form.Set("webIndexPagePwdLogin", "1")
	form.Set("pwdType", "1")
	form.Set("clientId", "1003")
	form.Set("authType", "2")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, y139MailLoginBase, strings.NewReader(form.Encode()))
	if err != nil {
		return "", cookies, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Origin", "https://mail.10086.cn")
	req.Header.Set("Referer", fmt.Sprintf("https://mail.10086.cn/default.html?&s=1&v=0&u=%s&m=1&ec=S001&resource=indexLogin&clientid=1003&auto=on&cguid=%s&mtime=45",
		base64.StdEncoding.EncodeToString([]byte(username)), cguid))
	req.Header.Set("User-Agent", y139BrowserUA)
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	if cookies != "" {
		req.Header.Set("Cookie", cookies)
	}

	// 不跟随跳转：sid 在 302 的 Location 里
	client := &http.Client{
		Timeout: 20 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", cookies, fmt.Errorf("邮箱登录请求失败: %w", err)
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))

	merged := y139MergeCookies(cookies, resp.Cookies())
	sid := ""
	loc := resp.Header.Get("Location")
	if loc != "" {
		if u, perr := url.Parse(loc); perr == nil {
			sid = u.Query().Get("sid")
		}
		// 10086 邮箱登录风控：ec 参数是错误码，S025/S035/MW0016/PML401010062 可走短信验证
		if sid == "" {
			if risk := y139RiskCodeOf(loc); risk != "" {
				scene, ok := y139RiskScene(risk)
				if !ok {
					return "", merged, fmt.Errorf("邮箱登录失败（ec=%s，暂不支持自动验证）：请在浏览器登录 mail.10086.cn 后把完整 Cookie 贴到「邮箱 Cookie」重试", risk)
				}
				y139SMSMu.Lock()
				y139SMSPre = &y139SMSState{RiskCode: risk, Username: username, Cookies: merged}
				y139SMSMu.Unlock()
				s.Log.Warn(logger.ModCloud139, "[移动云盘] 邮箱登录触发风控 ec=%s（短信场景 %d）", risk, scene)
				return "", merged, &y139RiskError{RiskCode: risk, Scene: scene}
			}
		}
	}
	if sid == "" {
		for _, c := range resp.Cookies() {
			if c.Name == "Os_SSo_Sid" || c.Name == "sid" {
				sid = c.Value
				break
			}
		}
	}
	if sid == "" {
		return "", merged, fmt.Errorf("邮箱登录失败：未获取到 sid（HTTP %d）%s",
			resp.StatusCode, y139LoginFailureDetail(resp, loc, bodyBytes))
	}
	return sid, merged, nil
}

// y139Step2 用 sid + RMKEY 换 artifact。
func (s *Services) y139Step2(ctx context.Context, sid, cookies string) (string, error) {
	rmkey := y139CookieValue(cookies, "RMKEY")
	if rmkey == "" {
		return "", fmt.Errorf("邮箱 Cookie 中缺少 RMKEY：请从浏览器复制 mail.10086.cn 的完整 Cookie 后重试")
	}
	cguid := strconv.FormatInt(time.Now().UnixMilli(), 10)
	u := fmt.Sprintf("%s?func=%s&sid=%s&cguid=%s", y139ArtifactBase, url.QueryEscape("umc:getArtifact"), url.QueryEscape(sid), cguid)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Cookie", "RMKEY="+rmkey)
	req.Header.Set("Content-Type", "text/xml; charset=utf-8")
	req.Header.Set("User-Agent", "okhttp/4.12.0")
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("换取 artifact 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		Var struct {
			Artifact string `json:"artifact"`
		} `json:"var"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil || parsed.Var.Artifact == "" {
		return "", fmt.Errorf("换取 artifact 失败（响应无法解析，HTTP %d）: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return parsed.Var.Artifact, nil
}

// y139Step3 单点登录换取 Authorization。
func (s *Services) y139Step3(ctx context.Context, username, artifact string) (*y139Auth, error) {
	payload := map[string]any{
		"clientkey_decrypt": "l3TryM&Q+X7@dzwk)qP",
		"clienttype":        "886",
		"cpid":              "507",
		"dycpwd":            artifact,
		"extInfo":           map[string]any{"ifOpenAccount": "0"},
		"loginMode":         "0",
		"msisdn":            username,
		"pintype":           "13",
		"secinfo":           strings.ToUpper(y139SHA1("fetion.com.cn:" + artifact)),
		"version":           "20250901",
	}
	headers := map[string]string{
		"hcy-cool-flag":       "1",
		"x-huawei-channelsrc": "10246600",
		"x-sdk-channelsrc":    "",
		"x-mm-source":         "0",
		"x-useragent":         "android|23116PN5BC|android15|1.2.6|||1440x3200|10246600",
		"x-deviceinfo":        "4|127.0.0.1|5|1.2.6|Xiaomi|23116PN5BC||02-00-00-00-00-00|android 15|1440x3200|android|||",
		"content-type":        "text/plain;charset=UTF-8",
		"user-agent":          "okhttp/3.12.2",
	}
	layer1, err := s.y139EncryptedRequest(ctx, y139ThirdLoginBase, payload, headers, y139Key1)
	if err != nil {
		return nil, err
	}
	var first struct {
		Data string `json:"data"`
	}
	if err := json.Unmarshal(layer1, &first); err != nil || first.Data == "" {
		return nil, fmt.Errorf("单点登录响应缺少 data: %s", truncate(string(layer1), 200))
	}
	key2, err := hex.DecodeString(y139Key2)
	if err != nil {
		return nil, err
	}
	inner, err := hex.DecodeString(first.Data)
	if err != nil {
		return nil, fmt.Errorf("单点登录 data 非 hex: %w", err)
	}
	final, err := y139AESECBDecrypt(inner, key2)
	if err != nil {
		return nil, fmt.Errorf("单点登录二次解密失败: %w", err)
	}
	var out struct {
		AuthToken    string `json:"authToken"`
		Account      string `json:"account"`
		UserDomainID string `json:"userDomainId"`
	}
	if err := json.Unmarshal(final, &out); err != nil || out.AuthToken == "" {
		return nil, fmt.Errorf("单点登录结果解析失败（authToken 缺失）: %s", truncate(string(final), 300))
	}
	auth := &y139Auth{
		Authorization: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("pc:%s:%s", out.Account, out.AuthToken))),
		Account:       out.Account,
		UserDomainID:  out.UserDomainID,
		ExpiresAt:     y139TokenExpiry(out.AuthToken),
	}
	return auth, nil
}

// y139VerifyAuthorization 用给定令牌请求路由策略 + 根目录列表，确认可用后落盘。
func (s *Services) y139VerifyAuthorization(ctx context.Context, authorization string) (*y139Auth, error) {
	auth := &y139Auth{
		Authorization: authorization,
		Account:       y139AuthorizationAccount(authorization),
		ExpiresAt:     y139AuthorizationExpiry(authorization),
	}
	host, err := s.y139RouteHost(ctx, authorization)
	if err != nil {
		return nil, err
	}
	if _, err := s.y139ListFiles(ctx, authorization, host, "/"); err != nil {
		return nil, err
	}
	if err := s.saveY139Auth(auth); err != nil {
		return nil, err
	}
	return auth, nil
}

// Yun139Verify 校验当前令牌（无令牌时用配置里的 authorization 字段）。
func (s *Services) Yun139Verify(ctx context.Context) (map[string]any, error) {
	auth, err := s.loadY139Auth()
	if err != nil {
		return nil, err
	}
	authorization := ""
	if auth != nil {
		authorization = auth.Authorization
	}
	if authorization == "" {
		authorization = strings.TrimSpace(toString(s.GetConfig(Drive139)["authorization"]))
	}
	if authorization == "" {
		return nil, fmt.Errorf("没有可校验的授权令牌，请先登录或填写「授权令牌」")
	}
	if _, err := s.y139VerifyAuthorization(ctx, authorization); err != nil {
		return nil, err
	}
	return s.Yun139Status(), nil
}

// Yun139Files 列出根目录文件（登录校验 / 后续文件管理复用）。
func (s *Services) Yun139Files(ctx context.Context, parentID string) (map[string]any, error) {
	auth, err := s.loadY139Auth()
	if err != nil || auth == nil || auth.Authorization == "" {
		return nil, fmt.Errorf("未登录移动云盘，请先完成账号密码登录")
	}
	host, err := s.y139RouteHost(ctx, auth.Authorization)
	if err != nil {
		return nil, err
	}
	return s.y139ListFiles(ctx, auth.Authorization, host, parentID)
}

// ---------------- 令牌续期 ----------------

// Yun139Refresh 令牌剩余不足 15 天时续期；续期失败回退账号密码登录。
func (s *Services) Yun139Refresh(ctx context.Context) error {
	auth, err := s.loadY139Auth()
	if err != nil || auth == nil || auth.Authorization == "" {
		return nil
	}
	exp := auth.ExpiresAt
	if exp == 0 {
		exp = y139AuthorizationExpiry(auth.Authorization)
	}
	remaining := exp - time.Now().UnixMilli()
	if remaining > 15*24*3600*1000 {
		return nil // 剩余超过 15 天，无需续期
	}

	decoded, err := base64.StdEncoding.DecodeString(auth.Authorization)
	if err != nil {
		return s.y139Relogin(ctx, fmt.Errorf("令牌解码失败: %w", err))
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) < 3 {
		return s.y139Relogin(ctx, fmt.Errorf("令牌格式异常"))
	}
	account := parts[1]
	token := parts[2]

	body := fmt.Sprintf("<root><token>%s</token><account>%s</account><clienttype>656</clienttype></root>",
		y139EscapeXML(token), y139EscapeXML(account))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, y139RefreshBase, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/xml")
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return s.y139Relogin(ctx, fmt.Errorf("令牌续期请求失败: %w", err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var out struct {
		Return string `xml:"return"`
		Token  string `xml:"token"`
		Desc   string `xml:"desc"`
	}
	if err := xml.Unmarshal(raw, &out); err != nil || out.Return != "0" || out.Token == "" {
		return s.y139Relogin(ctx, fmt.Errorf("令牌续期失败(return=%s %s)", out.Return, out.Desc))
	}
	auth.Authorization = base64.StdEncoding.EncodeToString([]byte(parts[0] + ":" + account + ":" + out.Token))
	auth.ExpiresAt = y139TokenExpiry(out.Token)
	if err := s.saveY139Auth(auth); err != nil {
		return err
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 令牌已自动续期")
	return nil
}

func (s *Services) y139Relogin(ctx context.Context, cause error) error {
	s.Log.Warn(logger.ModCloud139, "[移动云盘] %v，尝试账号密码重新登录", cause)
	if _, err := s.Yun139Login(ctx); err != nil {
		return fmt.Errorf("%v；账号密码登录也失败: %w", cause, err)
	}
	return nil
}

// StartYun139KeepAlive 每 12 小时检查一次令牌（<15 天续期），续期失败回退密码登录。
func (s *Services) StartYun139KeepAlive(ctx context.Context) {
	s.Log.Info(logger.ModCloud139, "[移动云盘] 令牌自动续期已启动（每 12 小时检查，剩余 <15 天时续期）")
	go func() {
		timer := time.NewTimer(90 * time.Second)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		ticker := time.NewTicker(y139KeepAlive)
		defer ticker.Stop()
		for {
			cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
			_ = s.Yun139Refresh(cctx)
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// ---------------- 接口请求 ----------------

// y139RouteHost 查询个人云接入域名。
func (s *Services) y139RouteHost(ctx context.Context, authorization string) (string, error) {
	payload := map[string]any{
		"userInfo": map[string]any{
			"userType":    1,
			"accountType": 1,
			"accountName": y139AuthorizationAccount(authorization),
		},
		"modAddrType": 1,
	}
	body, err := s.y139SignedPost(ctx, y139RouteBase, payload, authorization)
	if err != nil {
		return "", err
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			RoutePolicyList []struct {
				ModName  string `json:"modName"`
				HttpsURL string `json:"httpsUrl"`
			} `json:"routePolicyList"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("路由策略解析失败: %s", truncate(string(body), 200))
	}
	if !out.Success {
		return "", fmt.Errorf("路由策略查询失败: %s", orDefault(out.Message, "未知错误"))
	}
	for _, p := range out.Data.RoutePolicyList {
		if p.ModName == "personal" && p.HttpsURL != "" {
			return p.HttpsURL, nil
		}
	}
	return "", fmt.Errorf("未获取到个人云接入地址（路由策略为空）")
}

// y139ListFiles 列出个人云目录（parentID 为空表示根目录）。
func (s *Services) y139ListFiles(ctx context.Context, authorization, host, parentID string) (map[string]any, error) {
	// 服务端要求 parentFileId 非空：空/root 统一成根目录 "/"
	parentID = y139CatalogID(parentID)
	payload := map[string]any{
		"imageThumbnailStyleList": []string{"Small", "Large"},
		"orderBy":                 "updated_at",
		"orderDirection":          "DESC",
		"pageInfo": map[string]any{
			"pageCursor": "",
			"pageSize":   100,
		},
		"parentFileId": parentID,
	}
	body, err := s.y139SignedPost(ctx, strings.TrimRight(host, "/")+"/file/list", payload, authorization)
	if err != nil {
		return nil, err
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Items []struct {
				FileID    string `json:"fileId"`
				Name      string `json:"name"`
				Type      string `json:"type"`
				Size      int64  `json:"size"`
				UpdatedAt string `json:"updatedAt"`
			} `json:"items"`
			NextPageCursor string `json:"nextPageCursor"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("文件列表解析失败: %s", truncate(string(body), 200))
	}
	if !out.Success {
		return nil, fmt.Errorf("文件列表请求失败: %s", orDefault(out.Message, "未知错误"))
	}
	files := make([]map[string]any, 0, len(out.Data.Items))
	for _, it := range out.Data.Items {
		files = append(files, map[string]any{
			"id":         it.FileID,
			"name":       it.Name,
			"is_dir":     it.Type == "folder",
			"size":       it.Size,
			"updated_at": it.UpdatedAt,
		})
	}
	return map[string]any{"parent_id": parentID, "count": len(files), "files": files}, nil
}

// y139SignedPost 发送带 Mcloud-Sign 签名的个人云请求。
func (s *Services) y139SignedPost(ctx context.Context, endpoint string, payload any, authorization string) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	ts := time.Now().Format("2006-01-02 15:04:05")
	randStr := y139RandomString(16)
	sign := y139Sign(string(raw), ts, randStr)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json;charset=UTF-8")
	req.Header.Set("Authorization", "Basic "+authorization)
	req.Header.Set("Caller", "web")
	req.Header.Set("Cms-Device", "default")
	req.Header.Set("Mcloud-Channel", "1000101")
	req.Header.Set("Mcloud-Client", "10701")
	req.Header.Set("Mcloud-Route", "001")
	req.Header.Set("Mcloud-Sign", fmt.Sprintf("%s,%s,%s", ts, randStr, sign))
	req.Header.Set("Mcloud-Version", "7.14.0")
	req.Header.Set("Origin", "https://yun.139.com")
	req.Header.Set("Referer", "https://yun.139.com/w/")
	req.Header.Set("x-DeviceInfo", "||9|7.14.0|chrome|120.0.0.0|||windows 10||zh-CN|||")
	req.Header.Set("x-huawei-channelSrc", "10000034")
	req.Header.Set("x-inner-ntwk", "2")
	req.Header.Set("x-m4c-caller", "PC")
	req.Header.Set("x-m4c-src", "10002")
	req.Header.Set("x-SvcType", "1")
	req.Header.Set("X-Yun-Api-Version", "v1")
	req.Header.Set("X-Yun-App-Channel", "10000034")
	req.Header.Set("X-Yun-Channel-Source", "10000034")
	req.Header.Set("X-Yun-Client-Info", "||9|7.14.0|chrome|120.0.0.0|||windows 10||zh-CN|||")
	req.Header.Set("X-Yun-Module-Type", "100")
	req.Header.Set("X-Yun-Svc-Type", "1")
	req.Header.Set("Inner-Hcy-Router-Https", "1")

	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return nil, fmt.Errorf("鉴权失败（HTTP %d），授权令牌可能已过期", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("请求失败（HTTP %d）: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// y139EncryptedRequest AES-CBC 加密请求体，响应同样解密（base64(iv||ciphertext)）。
func (s *Services) y139EncryptedRequest(ctx context.Context, endpoint string, payload any, headers map[string]string, aesKeyHex string) ([]byte, error) {
	key, err := hex.DecodeString(aesKeyHex)
	if err != nil {
		return nil, err
	}
	sorted, err := y139SortedJSON(payload)
	if err != nil {
		return nil, err
	}
	iv := make([]byte, aes.BlockSize)
	if _, err := crand.Read(iv); err != nil {
		return nil, err
	}
	enc, err := y139AESCBCEncrypt([]byte(sorted), key, iv)
	if err != nil {
		return nil, err
	}
	payloadB64 := base64.StdEncoding.EncodeToString(append(iv, enc...))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(payloadB64))
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("加密接口请求失败（HTTP %d）: %s", resp.StatusCode, truncate(string(body), 200))
	}
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		return trimmed, nil // 少数场景返回明文 JSON
	}
	decoded, err := base64.StdEncoding.DecodeString(string(trimmed))
	if err != nil || len(decoded) < aes.BlockSize {
		return nil, fmt.Errorf("加密响应解码失败: %s", truncate(string(body), 200))
	}
	return y139AESCBCDecrypt(decoded[aes.BlockSize:], key, decoded[:aes.BlockSize])
}

// ---------------- 工具函数 ----------------

// y139Sign Mcloud-Sign：字符排序 + base64 + 两段 MD5 再 MD5（大写）。
func y139Sign(body, ts, randStr string) string {
	encoded := y139EncodeURIComponent(body)
	chars := strings.Split(encoded, "")
	sort.Strings(chars)
	sum1 := y139MD5(base64.StdEncoding.EncodeToString([]byte(strings.Join(chars, ""))))
	sum2 := y139MD5(ts + ":" + randStr)
	return strings.ToUpper(y139MD5(sum1 + sum2))
}

func y139EncodeURIComponent(s string) string {
	r := url.QueryEscape(s)
	for _, kv := range [][2]string{{"+", "%20"}, {"%21", "!"}, {"%27", "'"}, {"%28", "("}, {"%29", ")"}, {"%2A", "*"}} {
		r = strings.ReplaceAll(r, kv[0], kv[1])
	}
	return r
}

func y139MD5(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func y139SHA1(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func y139AESCBCEncrypt(plaintext, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := y139PKCS7Pad(plaintext, block.BlockSize())
	out := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, padded)
	return out, nil
}

// y139AESECBEncrypt AES-ECB + PKCS7（139 单点登录第二层用 ECB，不是 CBC）。
func y139AESECBEncrypt(plaintext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	padded := y139PKCS7Pad(plaintext, block.BlockSize())
	out := make([]byte, len(padded))
	for bs := 0; bs < len(padded); bs += block.BlockSize() {
		block.Encrypt(out[bs:bs+block.BlockSize()], padded[bs:bs+block.BlockSize()])
	}
	return out, nil
}

// y139AESECBDecrypt AES-ECB + PKCS7 解密（每块独立解密，块间不做异或）。
func y139AESECBDecrypt(ciphertext, key []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%block.BlockSize() != 0 || len(ciphertext) == 0 {
		return nil, errors.New("密文长度不是块大小的整数倍")
	}
	out := make([]byte, len(ciphertext))
	for bs := 0; bs < len(ciphertext); bs += block.BlockSize() {
		block.Decrypt(out[bs:bs+block.BlockSize()], ciphertext[bs:bs+block.BlockSize()])
	}
	return y139PKCS7Unpad(out)
}

func y139AESCBCDecrypt(ciphertext, key, iv []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ciphertext)%block.BlockSize() != 0 {
		return nil, errors.New("密文长度不是块大小的整数倍")
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	return y139PKCS7Unpad(out)
}

func y139PKCS7Pad(data []byte, size int) []byte {
	n := size - len(data)%size
	return append(data, bytes.Repeat([]byte{byte(n)}, n)...)
}

func y139PKCS7Unpad(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return nil, errors.New("空数据")
	}
	n := int(data[len(data)-1])
	if n <= 0 || n > len(data) {
		return nil, errors.New("非法填充")
	}
	return data[:len(data)-n], nil
}

// y139SortedJSON 按 key 排序序列化（服务端签名要求）。
func y139SortedJSON(v any) (string, error) {
	switch val := v.(type) {
	case nil:
		return "null", nil
	case string:
		// 只有对象/数组形态的字符串才当内嵌 JSON 递归处理；
		// 否则必须保持字符串（手机号、版本号等纯数字字符串不能被转成数字）。
		trimmed := strings.TrimSpace(val)
		if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") {
			var inner any
			if err := json.Unmarshal([]byte(val), &inner); err == nil {
				return y139SortedJSON(inner)
			}
		}
		raw, err := json.Marshal(val)
		return string(raw), err
	case int:
		return strconv.Itoa(val), nil
	case int64:
		return strconv.FormatInt(val, 10), nil
	case float64:
		return strconv.FormatFloat(val, 'f', -1, 64), nil
	case bool:
		return strconv.FormatBool(val), nil
	case []any:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			p, err := y139SortedJSON(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, p)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case []string:
		parts := make([]string, 0, len(val))
		for _, item := range val {
			parts = append(parts, `"`+item+`"`)
		}
		return "[" + strings.Join(parts, ",") + "]", nil
	case map[string]any:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		parts := make([]string, 0, len(keys))
		for _, k := range keys {
			p, err := y139SortedJSON(val[k])
			if err != nil {
				return "", err
			}
			parts = append(parts, `"`+k+`":`+p)
		}
		return "{" + strings.Join(parts, ",") + "}", nil
	default:
		raw, err := json.Marshal(val)
		return string(raw), err
	}
}

func y139RandomString(n int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	buf := make([]byte, n)
	if _, err := crand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	for i := range buf {
		buf[i] = charset[int(buf[i])%len(charset)]
	}
	return string(buf)
}

// y139AuthorizationAccount 从 Authorization 里解析账号。
func y139AuthorizationAccount(authorization string) string {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(authorization, "Basic ")))
	if err != nil {
		return ""
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) < 3 {
		return ""
	}
	return parts[1]
}

// y139AuthorizationExpiry 从 Authorization 里解析过期时间（毫秒），未知返回 0。
func y139AuthorizationExpiry(authorization string) int64 {
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(strings.TrimPrefix(authorization, "Basic ")))
	if err != nil {
		return 0
	}
	parts := strings.Split(string(decoded), ":")
	if len(parts) < 3 {
		return 0
	}
	return y139TokenExpiry(parts[2])
}

// y139TokenExpiry 从 authToken 的 "a|b|c|<expiryMs>|..." 里取过期毫秒。
func y139TokenExpiry(token string) int64 {
	seg := strings.Split(token, "|")
	if len(seg) < 4 {
		return 0
	}
	n, err := strconv.ParseInt(seg[3], 10, 64)
	if err != nil || n < 1e12 {
		return 0
	}
	return n
}

// y139LoginFailureDetail 汇总登录失败时的关键响应信息（脱敏），便于定位账号密码错误/风控。
func y139LoginFailureDetail(resp *http.Response, loc string, body []byte) string {
	parts := make([]string, 0, 3)
	if loc != "" {
		parts = append(parts, "Location="+y139SanitizeURL(loc))
	}
	if names := y139SetCookieNames(resp.Cookies()); len(names) > 0 {
		parts = append(parts, "Set-Cookie=["+strings.Join(names, ",")+"]")
	}
	if snippet := strings.TrimSpace(string(body)); snippet != "" {
		parts = append(parts, "body="+truncate(snippet, 180))
	}
	if len(parts) == 0 {
		return "（响应既无跳转也无 Cookie，通常是账号或密码错误）"
	}
	return "；" + strings.Join(parts, "；") + "；若出现 code/errCode 等参数，说明触发了 10086 风控，请在浏览器登录一次 mail.10086.cn 后把完整 Cookie 贴到「邮箱 Cookie」再试"
}

// y139SanitizeURL 隐藏 URL 里敏感参数的值，仅保留前 4 位。
func y139SanitizeURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return truncate(raw, 200)
	}
	q := u.Query()
	for key := range q {
		lower := strings.ToLower(key)
		if strings.Contains(lower, "sid") || strings.Contains(lower, "token") ||
			strings.Contains(lower, "passport") || strings.Contains(lower, "code") || strings.Contains(lower, "rmkey") {
			v := q.Get(key)
			if len(v) > 4 {
				v = v[:4] + "REDACTED"
			}
			q.Set(key, v)
		}
	}
	u.RawQuery = q.Encode()
	out := u.String()
	return truncate(out, 300)
}

func y139SetCookieNames(cs []*http.Cookie) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	return names
}

func y139CookieValue(cookies, name string) string {
	for _, part := range strings.Split(cookies, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// y139MergeCookies 把响应 Set-Cookie 合并进 Cookie 头（保持原有顺序，覆盖同名）。
func y139MergeCookies(existing string, set []*http.Cookie) string {
	values := map[string]string{}
	order := []string{}
	for _, part := range strings.Split(existing, ";") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		if _, seen := values[k]; !seen {
			order = append(order, k)
		}
		values[k] = strings.TrimSpace(v)
	}
	for _, c := range set {
		if _, seen := values[c.Name]; !seen {
			order = append(order, c.Name)
		}
		values[c.Name] = c.Value
	}
	parts := make([]string, 0, len(order))
	for _, k := range order {
		parts = append(parts, k+"="+values[k])
	}
	return strings.Join(parts, "; ")
}

func y139EscapeXML(s string) string {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(s))
	return buf.String()
}

// y139LoggedIn 是否已有可用的移动云盘令牌（用于判断能否执行真实转存）。
func (s *Services) y139LoggedIn() bool {
	auth, err := s.loadY139Auth()
	return err == nil && auth != nil && auth.Authorization != ""
}
