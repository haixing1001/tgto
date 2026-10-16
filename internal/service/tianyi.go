package service

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"tgto/internal/logger"
)

// 天翼云盘（189）扫码登录。
// 参考 OpenList 189pc 驱动与天翼开放登录接口实现：
//   1. unifyLoginForPC 跳转链拿到 lt/reqId（作为后续请求的鉴权头）
//   2. appConf.do 拿 paramId
//   3. getUUID.do 生成扫码 uuid/encryuuid，二维码内容为 uuid
//   4. qrcodeLoginState.do 轮询扫码状态，成功返回 redirectUrl
//   5. getSessionForPC.action 用 redirectUrl 换取会话（sessionKey/accessToken）

const (
	tyAPPID      = "9317140619"
	tyClientType = "10020"
	tyQRClient   = "1" // 扫码轮询使用与密码登录不同的 clientType
	tyReturnURL  = "https://m.cloud.189.cn/zhuanti/2020/loginErrorPc/index.html"
	tyWebURL     = "https://cloud.189.cn"
	tyAuthURL    = "https://open.e.189.cn"
	tyAPIURL     = "https://api.cloud.189.cn"
	tyQRExpiry   = 120 * time.Second // 二维码有效期
	tyPollGap    = 2 * time.Second   // 轮询间隔
)

// tyQRState 扫码状态查询返回。
// 天翼服务端在不同接口/场景下返回 result 或 status 字段，两者都需兼容。
type tyQRState struct {
	Result      int    `json:"result"`
	Status      int    `json:"status"`
	RedirectUrl string `json:"redirectUrl"`
	ToUrl       string `json:"toUrl"`
	Msg         string `json:"msg"`
}

// tyCode 取有效的状态码（兼容 result / status 两种字段）。
func (s *tyQRState) tyCode() int {
	if s.Result != 0 {
		return s.Result
	}
	return s.Status
}

// tyRedirect 取有效的跳转地址（兼容 redirectUrl / toUrl）。
func (s *tyQRState) tyRedirect() string {
	if s.RedirectUrl != "" {
		return s.RedirectUrl
	}
	return s.ToUrl
}

// tySession 扫码成功换取的会话（持久化到 DataDir/tianyi_session.json）。
type tySession struct {
	SessionKey    string `json:"session_key"`
	SessionSecret string `json:"session_secret"`
	LoginName     string `json:"login_name"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	KeepAlive     int    `json:"keep_alive"`
	UpdatedAt     int64  `json:"updated_at"`
}

// tyQRSession 一次扫码流程的会话状态（内存态，进程内单例）。
type tyQRSession struct {
	client   *http.Client
	lt       string
	reqID    string
	paramID  string
	uuid     string // 裸 uuid（二维码内容）
	encryuuid string
	referer  string
	created  time.Time
}

var (
	tyMu       sync.Mutex
	tyQR       *tyQRSession
)

// tyCookieClient 新建带独立 cookie jar 的 HTTP 客户端（15s 超时）。
func tyCookieClient() (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	return &http.Client{
		Timeout:   20 * time.Second,
		Jar:       jar,
		Transport: sharedHTTPClient.Transport,
	}, nil
}

// tyUserFinger 设备指纹，OpenList 参考实现为 10 位数字（1e9 ~ 1e10）。
func tyUserFinger() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%9000000000+1000000000)
	}
	n := int64(binary.BigEndian.Uint64(buf))
	if n < 0 {
		n = -n
	}
	return fmt.Sprintf("%d", n%9000000000+1000000000)
}

// tyPostForm 以表单方式 POST 并返回响应体文本。
func tyPostForm(ctx context.Context, client *http.Client, endpoint string, form url.Values, headers map[string]string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, stringsReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return tyReadBody(resp), nil
}

// tyReadBody 读取限长响应体文本（最多 4MB）。
func tyReadBody(resp *http.Response) string {
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return string(data)
}

// tyInitBase 走登录跳转链，拿到 lt/reqId 并以最终登录页作为 Referer。
func (s *Services) tyInitBase(ctx context.Context, client *http.Client) (lt, reqID, referer string, err error) {
	finger := tyUserFinger()
	u := fmt.Sprintf("%s/unifyLoginForPC.action?appId=%s&clientType=%s&returnURL=%s&timeStamp=%d",
		tyWebURL, tyAPPID, tyClientType, url.QueryEscape(tyReturnURL), time.Now().UnixMilli())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return "", "", "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	resp.Body.Close()
	// 跟随 302 到最终登录页（open.e.189.cn/.../index.html），从 URL 提取 lt/reqId。
	final := resp.Request.URL.String()
	fu, perr := url.Parse(final)
	if perr != nil {
		return "", "", "", perr
	}
	lt = fu.Query().Get("lt")
	reqID = fu.Query().Get("reqId")
	if lt == "" || reqID == "" {
		return "", "", "", fmt.Errorf("登录页未返回 lt/reqId（%s）", final)
	}
	_ = finger
	return lt, reqID, final, nil
}

// tyLoginHeaders logbox 接口公共请求头。
func tyLoginHeaders(lt, reqID, referer string) map[string]string {
	return map[string]string{
		"REQID":        reqID,
		"lt":           lt,
		"user-finger":  tyUserFinger(),
		"Referer":      referer,
		"Origin":       "https://open.e.189.cn",
		"Content-Type": "application/x-www-form-urlencoded",
	}
}

// tyAppConfParamId 拿 appConf 中的 paramId。
func (s *Services) tyAppConfParamId(ctx context.Context, client *http.Client, lt, reqID, referer string) (string, error) {
	body, err := tyPostForm(ctx, client, tyAuthURL+"/api/logbox/oauth2/appConf.do",
		url.Values{"version": {"2.0"}, "appKey": {tyAPPID}}, tyLoginHeaders(lt, reqID, referer))
	if err != nil {
		return "", err
	}
	var ac struct {
		Result string `json:"result"`
		Msg    string `json:"msg"`
		Data   struct {
			ParamId string `json:"paramId"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &ac); err != nil {
		return "", fmt.Errorf("appConf 解析失败: %s", body)
	}
	if ac.Result != "0" {
		return "", fmt.Errorf("appConf 失败: %s", ac.Msg)
	}
	return ac.Data.ParamId, nil
}

// TyQRStart 开始扫码登录：初始化会话、生成二维码。
func (s *Services) TyQRStart(ctx context.Context) (map[string]any, error) {
	client, err := tyCookieClient()
	if err != nil {
		return nil, err
	}
	lt, reqID, referer, err := s.tyInitBase(ctx, client)
	if err != nil {
		return nil, fmt.Errorf("初始化登录会话失败: %w", err)
	}
	paramID, err := s.tyAppConfParamId(ctx, client, lt, reqID, referer)
	if err != nil {
		return nil, fmt.Errorf("获取登录参数失败: %w", err)
	}
	// getUUID 生成扫码 uuid
	body, err := tyPostForm(ctx, client, tyAuthURL+"/api/logbox/oauth2/getUUID.do",
		url.Values{"appId": {tyAPPID}}, tyLoginHeaders(lt, reqID, referer))
	if err != nil {
		return nil, err
	}
	var gu struct {
		Result    int    `json:"result"`
		Msg       string `json:"msg"`
		UUID      string `json:"uuid"`
		Encryuuid string `json:"encryuuid"`
	}
	if err := json.Unmarshal([]byte(body), &gu); err != nil {
		return nil, fmt.Errorf("getUUID 解析失败: %s", body)
	}
	if gu.Result != 0 || gu.UUID == "" {
		return nil, fmt.Errorf("生成二维码失败: %s", gu.Msg)
	}
	// uuid 形如 https://open.e.189.cn/api/account/qrClinentLogin.do?paras=new_uuid%3Dxxx%7C9317140619
	// OpenList 参考实现：qrcodeLoginState.do 传完整 URL，二维码内容也是完整 URL
	rawU := gu.UUID
	sess := &tyQRSession{
		client:    client,
		lt:        lt,
		reqID:     reqID,
		paramID:   paramID,
		uuid:      rawU,
		encryuuid: gu.Encryuuid,
		referer:   referer,
		created:   time.Now(),
	}
	tyMu.Lock()
	tyQR = sess
	tyMu.Unlock()

	s.Log.Info(logger.ModTransfer, "[天翼云盘] 扫码会话已创建，uuid=%s", rawU)
	return map[string]any{
		"uuid":       rawU,
		"qr_url":     gu.UUID, // 完整扫码 URL，前端用于生成二维码
		"expires_in": int(tyQRExpiry.Seconds()),
		"status":     "waiting",
		"message":    "请使用天翼云盘 APP 扫描二维码",
	}, nil
}

// TyQRStatus 查询扫码状态并轮询推进；成功时换取会话并持久化。
func (s *Services) TyQRStatus(ctx context.Context) (map[string]any, error) {
	tyMu.Lock()
	sess := tyQR
	tyMu.Unlock()
	if sess == nil {
		return map[string]any{"status": "idle", "message": "暂无进行中的扫码，请点击「生成二维码」"}, nil
	}
	if time.Since(sess.created) > tyQRExpiry {
		tyMu.Lock()
		tyQR = nil
		tyMu.Unlock()
		return map[string]any{"status": "expired", "message": "二维码已过期，请重新生成"}, nil
	}

	state, err := s.tyCheckQRState(ctx, sess)
	if err != nil {
		return map[string]any{"status": "error", "message": err.Error()}, nil
	}
	code := state.tyCode()
	redirect := state.tyRedirect()
	switch code {
	case 0: // 登录成功
		if err := s.tyExchangeSession(ctx, sess, redirect); err != nil {
			return map[string]any{"status": "error", "message": "登录成功但换取会话失败: " + err.Error()}, nil
		}
		tyMu.Lock()
		tyQR = nil
		tyMu.Unlock()
		return map[string]any{"status": "success", "message": "扫码登录成功，天翼云盘 Cookie 已更新"}, nil
	case -106:
		return map[string]any{"status": "waiting", "message": "等待扫描二维码"}, nil
	case -11002:
		return map[string]any{"status": "scanned", "message": "已扫描，请在手机上确认登录"}, nil
	case -11001:
		tyMu.Lock()
		tyQR = nil
		tyMu.Unlock()
		return map[string]any{"status": "expired", "message": "二维码已过期，请重新生成"}, nil
	default:
		msg := state.Msg
		if msg == "" {
			msg = fmt.Sprintf("扫码登录失败（code=%d）", code)
		}
		return map[string]any{"status": "failed", "message": msg}, nil
	}
}

// tyCheckQRState 查询一次扫码状态。
func (s *Services) tyCheckQRState(ctx context.Context, sess *tyQRSession) (*tyQRState, error) {
	now := time.Now()
	form := url.Values{}
	form.Set("appId", tyAPPID)
	form.Set("clientType", tyQRClient)
	form.Set("returnUrl", tyReturnURL)
	form.Set("paramId", sess.paramID)
	form.Set("uuid", sess.uuid)
	form.Set("encryuuid", sess.encryuuid)
	form.Set("cb_SaveName", "3")
	form.Set("isOauth2", "false")
	form.Set("state", "")
	form.Set("date", now.Format("2006-01-0215:04:05.000"))
	form.Set("timeStamp", fmt.Sprintf("%d", now.UnixMilli()))
	body, err := tyPostForm(ctx, sess.client, tyAuthURL+"/api/logbox/oauth2/qrcodeLoginState.do", form, tyLoginHeaders(sess.lt, sess.reqID, sess.referer))
	if err != nil {
		return nil, err
	}
	var st tyQRState
	if err := json.Unmarshal([]byte(body), &st); err != nil {
		return nil, fmt.Errorf("扫码状态解析失败: %s", body)
	}
	return &st, nil
}

// tyExchangeSession 用扫码成功的 redirectUrl 换取会话并持久化。
func (s *Services) tyExchangeSession(ctx context.Context, sess *tyQRSession, redirectURL string) error {
	if redirectURL == "" {
		return fmt.Errorf("登录成功但未返回 redirectUrl")
	}
	// getSessionForPC.action：带 redirectURL 换取 sessionKey/accessToken。
	// getSessionForPC.action：OpenList 参考实现用 POST + returnType=JSON。
	full := fmt.Sprintf("%s/getSessionForPC.action?clientType=TELEPC&version=7.2.4.0&channelId=web_cloud.189.cn&rand=%d_%d&returnType=JSON",
		tyAPIURL, time.Now().UnixNano()%100000, time.Now().UnixNano())
	form := url.Values{}
	form.Set("redirectURL", redirectURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, full, stringsReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Request-ID", tyUUID())
	resp, err := sess.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := tyReadBody(resp)
	var us struct {
		ResCode       int    `json:"res_code"`
		ResMessage    string `json:"res_message"`
		LoginName     string `json:"loginName"`
		SessionKey    string `json:"sessionKey"`
		SessionSecret string `json:"sessionSecret"`
		KeepAlive     int    `json:"keepAlive"`
		AccessToken   string `json:"accessToken"`
		RefreshToken  string `json:"refreshToken"`
	}
	if err := json.Unmarshal([]byte(body), &us); err != nil {
		return fmt.Errorf("会话解析失败: %s", body)
	}
	if us.ResCode != 0 || us.SessionKey == "" {
		msg := us.ResMessage
		if msg == "" {
			msg = body
		}
		return fmt.Errorf("换取会话失败: %s", msg)
	}
	session := &tySession{
		SessionKey:    us.SessionKey,
		SessionSecret: us.SessionSecret,
		LoginName:     us.LoginName,
		AccessToken:   us.AccessToken,
		RefreshToken:  us.RefreshToken,
		KeepAlive:     us.KeepAlive,
		UpdatedAt:     time.Now().Unix(),
	}
	if err := s.saveTySession(session); err != nil {
		return err
	}
	// 同步账号名到配置分区，便于前端展示。
	if us.LoginName != "" {
		_, _ = s.SaveConfig(DriveTianYi, map[string]any{"account": us.LoginName})
	}
	s.Log.Info(logger.ModTransfer, "[天翼云盘] 扫码登录成功，账号=%s", us.LoginName)
	return nil
}

// TyQRCancel 取消进行中的扫码会话。
func (s *Services) TyQRCancel() map[string]any {
	tyMu.Lock()
	tyQR = nil
	tyMu.Unlock()
	return map[string]any{"status": "cancelled", "message": "已取消扫码"}
}

// TyStatus 返回天翼云盘扫码登录状态。
func (s *Services) TyStatus() map[string]any {
	sess, err := s.loadTySession()
	if err != nil || sess == nil || sess.SessionKey == "" {
		tyMu.Lock()
		inProgress := tyQR != nil
		tyMu.Unlock()
		return map[string]any{"logged_in": false, "status": "未登录，请点击「生成二维码」扫码登录", "in_progress": inProgress}
	}
	cfg := s.GetConfig(DriveTianYi)
	account, _ := cfg["account"].(string)
	if account == "" {
		account = sess.LoginName
	}
	return map[string]any{
		"logged_in":   true,
		"status":      "已登录",
		"account":     account,
		"updated_at":  sess.UpdatedAt,
		"in_progress": false,
	}
}

// TyClearLogin 清除天翼云盘扫码登录会话。
func (s *Services) TyClearLogin() (map[string]any, error) {
	tyMu.Lock()
	tyQR = nil
	tyMu.Unlock()
	_ = os.Remove(s.tySessionPath())
	return map[string]any{"logged_in": false, "status": "已清除天翼云盘登录信息"}, nil
}

// ---------------- 持久化 ----------------

func (s *Services) tySessionPath() string {
	return filepath.Join(s.DataDir, "tianyi_session.json")
}

func (s *Services) loadTySession() (*tySession, error) {
	raw, err := os.ReadFile(s.tySessionPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	sess := &tySession{}
	if err := json.Unmarshal(raw, sess); err != nil {
		return nil, err
	}
	return sess, nil
}

func (s *Services) saveTySession(sess *tySession) error {
	if err := os.MkdirAll(s.DataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(sess, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.tySessionPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.tySessionPath())
}

// ---------------- 工具 ----------------

// tyUUID 生成随机 UUID（用于 X-Request-ID）。
func tyUUID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", buf[0:4], buf[4:6], buf[6:8], buf[8:10], buf[10:16])
}

// tyRSAEncrypt 用公钥加密（账号密码登录备用）。
func tyRSAEncrypt(pubKeyPEM, data string) (string, error) {
	block, _ := pem.Decode([]byte(pubKeyPEM))
	if block == nil {
		return "", fmt.Errorf("公钥解析失败")
	}
	pub, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return "", err
	}
	enc, err := rsa.EncryptPKCS1v15(rand.Reader, pub.(*rsa.PublicKey), []byte(data))
	if err != nil {
		return "", err
	}
	return strings.ToUpper(hex.EncodeToString(enc)), nil
}
