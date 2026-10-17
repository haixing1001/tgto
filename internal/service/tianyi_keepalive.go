package service

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"tgto/internal/logger"
)

// 天翼云盘会话保活（参考 OpenList drivers/189pc 的实现顺序）：
//  1. GET  {API}/keepUserSession.action                  → 续期当前 SessionKey（主路径）
//  2. GET  {API}/getSessionForPC.action?accessToken=...  → 用 accessToken 换新 SessionKey/SessionSecret
//  3. POST {AUTH}/api/oauth2/refreshToken.do             → 用 refreshToken 换新 accessToken，再回到 2
// 任何一步成功即刷新本地 updated_at；全部失败只记 Warn，保留旧会话等用户重新扫码。
// 注意：天翼扫码登录返回的 sessionKey 会过期，没有保活时表现为「昨天还能用，今天全报未登录」。

// 业务/鉴权基址做成变量，便于测试用 httptest 服务器替换（线上恒为官方域名）。
var (
	tyAPIBase  = tyAPIURL
	tyAuthBase = tyAuthURL
)

const (
	tyKeepAliveInterval = 10 * time.Minute // OpenList 用 5 分钟；NAS 场景 10 分钟足够且更省唤醒
	tyKeepAliveDelay    = 45 * time.Second // 启动后延迟首轮，避开初始化竞争
	tyPCClientType      = "TELEPC"
	tyPCVersion         = "7.2.4.0"
	tyPCChannelID       = "web_cloud.189.cn"
)

// 上一次保活是否成功，用于抑制「每 10 分钟一条成功日志」
var tyKeepAliveHealthy atomic.Bool

// tyFlexCode 兼容天翼接口把错误码写成数字（0/-1）或字符串（"UserInvalidOpenToken"）两种形式。
type tyFlexCode struct{ raw string }

func (c *tyFlexCode) UnmarshalJSON(b []byte) error {
	c.raw = strings.Trim(strings.TrimSpace(string(b)), `"`)
	return nil
}

func (c tyFlexCode) Int() int {
	n, _ := strconv.Atoi(c.raw)
	return n
}

// InvalidToken 是否为 accessToken 失效（此时应走 refreshToken）。
func (c tyFlexCode) InvalidToken() bool {
	return strings.Contains(strings.ToLower(c.raw), "invalidopentoken")
}

func (c tyFlexCode) String() string { return c.raw }

// StartTyKeepAlive 启动天翼会话保活循环。
func (s *Services) StartTyKeepAlive(ctx context.Context) {
	s.Log.Info(logger.ModTianYi, "[天翼云盘] 会话保活已启动（每 %d 分钟一次）", int(tyKeepAliveInterval/time.Minute))
	go func() {
		timer := time.NewTimer(tyKeepAliveDelay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		ticker := time.NewTicker(tyKeepAliveInterval)
		defer ticker.Stop()
		for {
			s.tyKeepAliveOnce(ctx)
			select {
			case <-ctx.Done():
				s.Log.Info(logger.ModTianYi, "[天翼云盘] 会话保活已停止")
				return
			case <-ticker.C:
			}
		}
	}()
}

// tyKeepAliveOnce 执行一轮保活（未登录时直接跳过）。
func (s *Services) tyKeepAliveOnce(ctx context.Context) {
	sess, err := s.loadTySession()
	if err != nil || sess == nil || sess.SessionKey == "" {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	// 1. 主路径：续期当前 SessionKey
	if err := s.tyKeepUserSession(cctx, sess); err == nil {
		sess.Invalid = false
		s.touchTySession(sess)
		if !tyKeepAliveHealthy.Swap(true) {
			s.Log.Info(logger.ModTianYi, "[天翼云盘] 会话保活成功（SessionKey 有效）")
		}
		return
	} else {
		s.Log.Warn(logger.ModTianYi, "[天翼云盘] keepUserSession 失败: %v，尝试刷新会话", err)
	}

	// 2/3. 刷新会话（内部会按需走 refreshToken）
	if err := s.TyRefreshSession(cctx); err != nil {
		tyKeepAliveHealthy.Store(false)
		if tyIsAuthError(err) {
			// 鉴权彻底失效：标记会话无效，前端绿点转灰提示重新扫码。
			// 注意不动 updated_at——它表示「最后一次成功续期时间」。
			sess.Invalid = true
			if serr := s.saveTySession(sess); serr != nil {
				s.Log.Warn(logger.ModTianYi, "[天翼云盘] 失效标记保存失败: %v", serr)
			}
		}
		s.Log.Warn(logger.ModTianYi, "[天翼云盘] 会话刷新失败: %v，需要重新扫码登录", err)
		return
	}
	tyKeepAliveHealthy.Store(true)
	s.Log.Info(logger.ModTianYi, "[天翼云盘] 会话已自动刷新（新的 SessionKey 已保存）")
}

// tyKeepUserSession 调用 keepUserSession.action 续期会话。
func (s *Services) tyKeepUserSession(ctx context.Context, sess *tySession) error {
	decoded, err := s.tyBusinessGet(ctx, sess, "/keepUserSession.action", nil)
	if err != nil {
		return err
	}
	if decoded == nil {
		return fmt.Errorf("响应无法解析")
	}
	if tyNeedLogin(decoded) {
		return fmt.Errorf("SessionKey 已失效")
	}
	if code, ok := tyResCode(decoded); ok && code != 0 {
		msg, _ := decoded["res_message"].(string)
		return fmt.Errorf("res_code=%d %s", code, strings.TrimSpace(msg))
	}
	return nil
}

// TyRefreshSession 用 accessToken 换新的 SessionKey/SessionSecret；
// accessToken 失效时自动用 refreshToken 换新 accessToken 后重试一次。
func (s *Services) TyRefreshSession(ctx context.Context) error {
	sess, err := s.loadTySession()
	if err != nil {
		return err
	}
	if sess == nil || sess.SessionKey == "" {
		return fmt.Errorf("未检测到天翼云盘登录信息")
	}
	err = s.tyRefreshSessionOnce(ctx, sess)
	if err == nil {
		return nil
	}
	// accessToken 失效 → 换 token 再试
	if sess.RefreshToken == "" {
		return fmt.Errorf("%v（且无 refreshToken，只能重新扫码）", err)
	}
	if rerr := s.tyRefreshAccessToken(ctx, sess); rerr != nil {
		return fmt.Errorf("%v；refreshToken 也失效: %v", err, rerr)
	}
	return s.tyRefreshSessionOnce(ctx, sess)
}

// tyRefreshSessionOnce 调 getSessionForPC.action（不签名，仅 X-Request-ID）。
func (s *Services) tyRefreshSessionOnce(ctx context.Context, sess *tySession) error {
	if sess.AccessToken == "" {
		return fmt.Errorf("会话缺少 accessToken")
	}
	q := url.Values{}
	q.Set("clientType", tyPCClientType)
	q.Set("version", tyPCVersion)
	q.Set("channelId", tyPCChannelID)
	q.Set("appId", tyAPPID)
	q.Set("accessToken", sess.AccessToken)
	q.Set("rand", fmt.Sprintf("%d_%d", time.Now().UnixNano()%100000, time.Now().UnixNano()))
	fullURL := tyAPIBase + "/getSessionForPC.action?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	req.Header.Set("Referer", tyWebURL)
	req.Header.Set("X-Request-ID", tyUUID())
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := tyReadBody(resp)

	var us struct {
		ResCode    tyFlexCode `json:"res_code"`
		Result     tyFlexCode `json:"result"`
		ResMessage string     `json:"res_message"`
		Msg        string     `json:"msg"`
		LoginName  string     `json:"loginName"`
		SessionKey string     `json:"sessionKey"`
		Secret     string     `json:"sessionSecret"`
		KeepAlive  int        `json:"keepAlive"`
	}
	if err := json.Unmarshal([]byte(body), &us); err != nil {
		return fmt.Errorf("刷新会话响应解析失败: %s", truncate(body, 200))
	}
	if us.ResCode.InvalidToken() {
		return fmt.Errorf("accessToken 已失效(%s)", us.ResCode)
	}
	if code := us.ResCode.Int(); code != 0 || us.SessionKey == "" {
		msg := strings.TrimSpace(us.ResMessage)
		if msg == "" {
			msg = strings.TrimSpace(us.Msg)
		}
		if msg == "" {
			msg = truncate(body, 200)
		}
		return fmt.Errorf("刷新会话失败(res_code=%s): %s", us.ResCode, msg)
	}
	sess.SessionKey = us.SessionKey
	sess.SessionSecret = us.Secret
	sess.Invalid = false
	if us.LoginName != "" {
		sess.LoginName = us.LoginName
	}
	if us.KeepAlive > 0 {
		sess.KeepAlive = us.KeepAlive
	}
	s.touchTySession(sess)
	if us.LoginName != "" {
		_, _ = s.SaveConfig(DriveTianYi, map[string]any{"account": us.LoginName})
	}
	return nil
}

// tyRefreshAccessToken 用 refreshToken 换新 accessToken（OAuth2 refresh_token 授权）。
func (s *Services) tyRefreshAccessToken(ctx context.Context, sess *tySession) error {
	form := url.Values{}
	form.Set("clientId", tyAPPID)
	form.Set("refreshToken", sess.RefreshToken)
	form.Set("grantType", "refresh_token")
	form.Set("format", "json")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tyAuthBase+"/api/oauth2/refreshToken.do", stringsReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Request-ID", tyUUID())
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := tyReadBody(resp)

	var tk struct {
		ResCode      tyFlexCode `json:"res_code"`
		Result       tyFlexCode `json:"result"`
		ResMessage   string     `json:"res_message"`
		Msg          string     `json:"msg"`
		AccessToken  string     `json:"accessToken"`
		RefreshToken string     `json:"refreshToken"`
		SessionKey   string     `json:"sessionKey"`
		Secret       string     `json:"sessionSecret"`
		KeepAlive    int        `json:"keepAlive"`
	}
	if err := json.Unmarshal([]byte(body), &tk); err != nil {
		return fmt.Errorf("refreshToken 响应解析失败: %s", truncate(body, 200))
	}
	if tk.AccessToken == "" {
		msg := strings.TrimSpace(tk.ResMessage)
		if msg == "" {
			msg = strings.TrimSpace(tk.Msg)
		}
		if msg == "" {
			msg = truncate(body, 200)
		}
		code := tk.ResCode
		if code.raw == "" {
			code = tk.Result
		}
		return fmt.Errorf("refreshToken 换取失败(result=%s): %s", code, msg)
	}
	sess.AccessToken = tk.AccessToken
	if tk.RefreshToken != "" {
		sess.RefreshToken = tk.RefreshToken
	}
	if tk.SessionKey != "" {
		sess.SessionKey = tk.SessionKey
		sess.SessionSecret = tk.Secret
	}
	if tk.KeepAlive > 0 {
		sess.KeepAlive = tk.KeepAlive
	}
	s.touchTySession(sess)
	return nil
}

// touchTySession 更新会话时间戳并落盘（保活成功的证据）。
func (s *Services) touchTySession(sess *tySession) {
	sess.UpdatedAt = time.Now().Unix()
	if err := s.saveTySession(sess); err != nil {
		s.Log.Warn(logger.ModTianYi, "[天翼云盘] 会话保存失败: %v", err)
	}
}

// tyIsAuthError 判断错误是否属于「登录态失效」（网络/解析类错误不算，避免误把绿点打灰）。
func tyIsAuthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, kw := range []string{"invalidopentoken", "sessionkey 已失效", "refreshtoken 换取失败", "刷新会话失败", "userinvalidopentoken"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// tyResCode 从业务响应里取 res_code（数字或字符串）。
func tyResCode(decoded map[string]any) (int, bool) {
	switch v := decoded["res_code"].(type) {
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n), true
		}
	case float64:
		return int(v), true
	case int:
		return v, true
	case string:
		var n int
		if _, err := fmt.Sscanf(v, "%d", &n); err == nil {
			return n, true
		}
	}
	return 0, false
}
