package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"tgto/internal/logger"
)

// 光鸭云盘 SMS 验证码登录。
// 端点与请求格式参考开源客户端 https://github.com/DDSRem-Dev/guangyaclient ：
//   - 人机验证初始化：POST https://account.guangyapan.com/v1/shield/captcha/init → captcha_token
//   - 发送验证码：POST /v1/auth/verification（x-captcha-token）→ verification_id
//   - 校验验证码：POST /v1/auth/verification/verify → verification_token
//   - 完成登录：POST /v1/auth/signin → access_token / refresh_token / expires_in
//   - 刷新 Token：POST /v1/auth/token（grant_type=refresh_token）
//   - 用户信息：GET /v1/user/me（Authorization: Bearer）
// access token / refresh token / 过期时间持久化到 DataDir/guangya_token.json。
//
// client_id 固定为参考客户端使用的公开值；域名按 ENV_GUANGYA_ACCOUNT_BASE /
// ENV_GUANGYA_API_BASE 环境变量可覆盖（默认生产环境）。

const (
	gyClientID = "aMe-8VSlkrbQXpUR"
	// Device Sign 前缀（参考客户端 x-device-sign = "wdi10." + did + 32 hex）。
	gyDeviceSignPrefix = "wdi10."
)

var (
	// 账号域名：验证码 / 登录 / 刷新（ENV_GUANGYA_ACCOUNT_BASE 可覆盖）。
	gyAccountBase = "https://account.guangyapan.com"
	// API 域名：文件 / 云下载 / 分享等业务接口（ENV_GUANGYA_API_BASE 可覆盖）。
	gyAPIBase = "https://api.guangyapan.com"
)

func init() {
	if base := strings.TrimSpace(os.Getenv("ENV_GUANGYA_ACCOUNT_BASE")); base != "" {
		gyAccountBase = strings.TrimRight(base, "/")
	}
	if base := strings.TrimSpace(os.Getenv("ENV_GUANGYA_API_BASE")); base != "" {
		gyAPIBase = strings.TrimRight(base, "/")
	}
}

// GyToken 光鸭登录凭证（持久化结构）。
type GyToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"` // Unix 秒
}

// gySmsSession 短信验证码会话状态（服务端内存）。
type gySmsSession struct {
	phone          string
	captchaToken   string
	verificationID string
	createdAt      time.Time
}

var (
	gyMu       sync.Mutex
	gyPending  = map[string]gySmsSession{}
	rePhoneGY  = regexp.MustCompile(`^\+?86\d{11}$|^1[3-9]\d{9}$`)
	reDigitsGY = regexp.MustCompile(`^\d{4,8}$`)
)

// gyTokenPath 返回光鸭 token 文件路径。
func (s *Services) gyTokenPath() string {
	return filepath.Join(s.DataDir, "guangya_token.json")
}

// gyConfigPhone 读取配置中的登录手机号（归一化为含国际区号格式）。
func (s *Services) gyConfigPhone() string {
	phone := strings.TrimSpace(toString(s.GetConfig(DriveGY)["phone"]))
	return gyNormalizePhone(phone)
}

// gyNormalizePhone 将手机号归一化为 "+86 13800138000" 形式。
func gyNormalizePhone(phone string) string {
	phone = strings.ReplaceAll(phone, " ", "")
	if phone == "" {
		return ""
	}
	if strings.HasPrefix(phone, "+86") {
		return phone
	}
	if strings.HasPrefix(phone, "86") && len(phone) == 13 {
		return "+" + phone
	}
	if rePhoneGY.MatchString(phone) {
		return "+86" + phone
	}
	return phone
}

// gyDeviceID 读取设备 ID（配置中无则随机生成）。
func (s *Services) gyDeviceID() string {
	if v := strings.TrimSpace(toString(s.GetConfig(DriveGY)["device_id"])); v != "" {
		return v
	}
	return gyRandomDeviceID()
}

// gyDeviceSign 生成 x-device-sign（"wdi10." + did + 32 位随机 hex）。
func gyDeviceSign(deviceID string) string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return gyDeviceSignPrefix + deviceID
	}
	return gyDeviceSignPrefix + deviceID + hex.EncodeToString(buf)
}

// gyAccountHeaders 构造账号域名的公共请求头（模拟 PC-Chrome 客户端）。
func (s *Services) gyAccountHeaders(deviceID string) map[string]string {
	return map[string]string{
		"accept":             "*/*",
		"content-type":       "application/json",
		"origin":             "https://www.guangyapan.com",
		"referer":            "https://www.guangyapan.com/",
		"user-agent":         "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36",
		"x-client-id":        gyClientID,
		"x-client-version":   "0.0.1",
		"x-device-id":        deviceID,
		"x-device-model":     "chrome%2F147.0.0.0",
		"x-device-name":      "PC-Chrome",
		"x-device-sign":      gyDeviceSign(deviceID),
		"x-net-work-type":    "NONE",
		"x-os-version":       "MacIntel",
		"x-platform-version": "1",
		"x-protocol-version": "301",
		"x-provider-name":    "NONE",
		"x-sdk-version":      "9.0.2",
	}
}

// gyStatus 返回光鸭登录状态（供前端状态行展示）。
func (s *Services) GyStatus() map[string]any {
	tok, err := s.loadGyToken()
	if err != nil || tok == nil || tok.AccessToken == "" {
		return map[string]any{"logged_in": false, "status": "未检测到已保存的光鸭登录信息"}
	}
	expired := tok.ExpiresAt > 0 && time.Now().Unix() >= tok.ExpiresAt
	status := "已登录"
	if expired {
		status = "Token 已过期，请点击「刷新 Token」"
	}
	return map[string]any{
		"logged_in":  !expired,
		"expired":    expired,
		"expires_at": tok.ExpiresAt,
		"status":     status,
	}
}

// GySendCode 发送短信验证码：先初始化人机验证获取 captcha_token，再发送验证码。
func (s *Services) GySendCode(ctx context.Context) (map[string]any, error) {
	phone := s.gyConfigPhone()
	if !rePhoneGY.MatchString(strings.TrimPrefix(phone, "+86")) {
		return nil, fmt.Errorf("请先在「光鸭云盘账号配置」中填写正确的 11 位登录手机号并保存")
	}
	deviceID := s.gyDeviceID()
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}

	// 1. 人机验证初始化
	initBody := fmt.Sprintf(`{"client_id":%q,"action":"POST:/v1/auth/verification","device_id":%q,"meta":{"phone_number":%q}}`,
		gyClientID, deviceID, phone)
	initResp, err := httpPostJSONWithHeaders(ctx, gyAccountBase+"/v1/shield/captcha/init", initBody, s.gyAccountHeaders(deviceID))
	if err != nil {
		s.Log.Warn(logger.ModGuangYa, "光鸭人机验证初始化失败: %v", err)
		return nil, fmt.Errorf("验证码初始化失败：%v", err)
	}
	initDecoded := gyDecodeBody(initResp)
	if initDecoded == nil {
		return nil, fmt.Errorf("人机验证响应无法解析（%s）", truncate(initResp, 120))
	}
	if u := gyStr(initDecoded, "url", ""); u != "" {
		return nil, fmt.Errorf("该手机号触发了人机验证，请稍后重试或先在浏览器完成验证")
	}
	captchaToken := gyStr(initDecoded, "captcha_token", "")
	if captchaToken == "" {
		return nil, fmt.Errorf("%s", gyErrMessage(initDecoded, "人机验证初始化失败"))
	}

	// 2. 发送验证码
	sendHeaders := s.gyAccountHeaders(deviceID)
	sendHeaders["x-captcha-token"] = captchaToken
	sendBody := fmt.Sprintf(`{"phone_number":%q,"target":"ANY","client_id":%q}`, phone, gyClientID)
	sendResp, err := httpPostJSONWithHeaders(ctx, gyAccountBase+"/v1/auth/verification", sendBody, sendHeaders)
	if err != nil {
		s.Log.Warn(logger.ModGuangYa, "光鸭验证码发送失败: %v", err)
		return nil, fmt.Errorf("验证码发送失败：%v", err)
	}
	sendDecoded := gyDecodeBody(sendResp)
	if sendDecoded == nil {
		return nil, fmt.Errorf("验证码发送响应无法解析（%s）", truncate(sendResp, 120))
	}
	if !gyIsSuccess(sendDecoded) {
		return nil, fmt.Errorf("%s", gyErrMessage(sendDecoded, "验证码发送失败"))
	}
	verificationID := gyStr(sendDecoded, "verification_id", "")
	if verificationID == "" {
		return nil, fmt.Errorf("发送响应缺少 verification_id（%s）", truncate(sendResp, 120))
	}
	gyMu.Lock()
	gyPending[phone] = gySmsSession{
		phone:          phone,
		captchaToken:   captchaToken,
		verificationID: verificationID,
		createdAt:      time.Now(),
	}
	gyMu.Unlock()
	s.Log.Info(logger.ModGuangYa, "光鸭短信验证码已发送至 %s", maskPhoneGY(phone))
	return map[string]any{"status": "验证码已发送，请查收短信"}, nil
}

// GyVerifyLogin 校验短信验证码并保存 Token。
func (s *Services) GyVerifyLogin(ctx context.Context, code string) (map[string]any, error) {
	code = strings.TrimSpace(code)
	if !reDigitsGY.MatchString(code) {
		return nil, fmt.Errorf("请输入正确的短信验证码")
	}
	phone := s.gyConfigPhone()
	if phone == "" {
		return nil, fmt.Errorf("请先填写登录手机号并保存配置")
	}
	gyMu.Lock()
	sess, ok := gyPending[phone]
	gyMu.Unlock()
	if !ok || time.Since(sess.createdAt) > 10*time.Minute {
		return nil, fmt.Errorf("验证码会话已过期，请重新点击「发送验证码」")
	}
	deviceID := s.gyDeviceID()
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}

	// 1. 校验验证码 → verification_token
	verifyBody := fmt.Sprintf(`{"verification_id":%q,"verification_code":%q,"client_id":%q}`,
		sess.verificationID, code, gyClientID)
	verifyResp, err := httpPostJSONWithHeaders(ctx, gyAccountBase+"/v1/auth/verification/verify", verifyBody, s.gyAccountHeaders(deviceID))
	if err != nil {
		s.Log.Warn(logger.ModGuangYa, "光鸭验证码校验失败: %v", err)
		return nil, fmt.Errorf("验证码校验失败：%v", err)
	}
	verifyDecoded := gyDecodeBody(verifyResp)
	if verifyDecoded == nil {
		return nil, fmt.Errorf("校验响应无法解析（%s）", truncate(verifyResp, 120))
	}
	if !gyIsSuccess(verifyDecoded) {
		return nil, fmt.Errorf("%s", gyErrMessage(verifyDecoded, "验证码校验失败"))
	}
	verificationToken := gyStr(verifyDecoded, "verification_token", "")
	if verificationToken == "" {
		return nil, fmt.Errorf("校验响应缺少 verification_token（%s）", truncate(verifyResp, 120))
	}

	// 2. 完成登录
	signinHeaders := s.gyAccountHeaders(deviceID)
	signinHeaders["x-captcha-token"] = sess.captchaToken
	signinBody := fmt.Sprintf(`{"verification_code":%q,"verification_token":%q,"username":%q,"client_id":%q}`,
		code, verificationToken, phone, gyClientID)
	signinResp, err := httpPostJSONWithHeaders(ctx, gyAccountBase+"/v1/auth/signin", signinBody, signinHeaders)
	if err != nil {
		s.Log.Warn(logger.ModGuangYa, "光鸭短信登录失败: %v", err)
		return nil, fmt.Errorf("验证并登录失败：%v", err)
	}
	tok, err := gyParseToken(signinResp)
	if err != nil {
		return nil, err
	}
	if err := s.saveGyToken(tok); err != nil {
		return nil, fmt.Errorf("Token 保存失败: %v", err)
	}
	gyMu.Lock()
	delete(gyPending, phone)
	gyMu.Unlock()
	s.Log.Info(logger.ModGuangYa, "光鸭短信登录成功（%s），Token 已持久化", maskPhoneGY(phone))
	return map[string]any{"logged_in": true, "status": "登录成功，Token 已保存"}, nil
}

// GyRefreshToken 刷新光鸭 Token。
func (s *Services) GyRefreshToken(ctx context.Context) (map[string]any, error) {
	tok, err := s.loadGyToken()
	if err != nil || tok == nil || tok.RefreshToken == "" {
		return nil, fmt.Errorf("未检测到已保存的光鸭登录信息，请先完成短信登录")
	}
	deviceID := s.gyDeviceID()
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	headers := s.gyAccountHeaders(deviceID)
	headers["x-action"] = "401"
	body := fmt.Sprintf(`{"client_id":%q,"grant_type":%q,"refresh_token":%q}`, gyClientID, "refresh_token", tok.RefreshToken)
	resp, err := httpPostJSONWithHeaders(ctx, gyAccountBase+"/v1/auth/token", body, headers)
	if err != nil {
		s.Log.Warn(logger.ModGuangYa, "光鸭 Token 刷新失败: %v", err)
		return nil, fmt.Errorf("刷新失败：%v（可尝试重新短信登录）", err)
	}
	refreshed, err := gyParseToken(resp)
	if err != nil {
		return nil, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tok.RefreshToken
	}
	if err := s.saveGyToken(refreshed); err != nil {
		return nil, fmt.Errorf("Token 保存失败: %v", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭 Token 已刷新并持久化")
	return map[string]any{"logged_in": true, "status": "Token 刷新成功"}, nil
}

// GyVerifyToken 校验 Token 有效性（获取用户信息）。
func (s *Services) GyVerifyToken(ctx context.Context) (map[string]any, error) {
	tok, err := s.loadGyToken()
	if err != nil || tok == nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("未检测到已保存的光鸭登录信息")
	}
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	if err := s.gyUserInfo(ctx, tok); err != nil {
		s.Log.Warn(logger.ModGuangYa, "光鸭 Token 校验失败: %v", err)
		return nil, fmt.Errorf("校验失败：%v", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭 Token 校验通过")
	return map[string]any{"logged_in": true, "status": "Token 校验通过"}, nil
}

// GyClearLogin 清除已保存的光鸭登录信息。
func (s *Services) GyClearLogin() (map[string]any, error) {
	if err := os.Remove(s.gyTokenPath()); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("清除失败: %v", err)
	}
	gyMu.Lock()
	gyPending = map[string]gySmsSession{}
	gyMu.Unlock()
	s.Log.Info(logger.ModGuangYa, "光鸭登录信息已清除")
	return map[string]any{"status": "已清除光鸭登录信息"}, nil
}

// gyUserInfo 获取当前登录用户信息（GET /v1/user/me）。
func (s *Services) gyUserInfo(ctx context.Context, tok *GyToken) error {
	endpoint := gyAccountBase + "/v1/user/me"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	deviceID := s.gyDeviceID()
	for k, v := range s.gyAccountHeaders(deviceID) {
		req.Header.Set(k, v)
	}
	req.Header.Set("authorization", "Bearer "+tok.AccessToken)
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body := gyReadBody(resp)
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if decoded := gyDecodeBody(body); decoded != nil {
		if errCode := gyErrCode(decoded); errCode != "" {
			return fmt.Errorf("%s", gyErrMessage(decoded, "接口返回失败"))
		}
	}
	return nil
}

// ---------------- 内部工具 ----------------

// loadGyToken 读取持久化的光鸭 Token。
func (s *Services) loadGyToken() (*GyToken, error) {
	raw, err := os.ReadFile(s.gyTokenPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	tok := &GyToken{}
	if err := json.Unmarshal(raw, tok); err != nil {
		return nil, err
	}
	return tok, nil
}

// saveGyToken 持久化光鸭 Token（0600 权限，原子写入）。
func (s *Services) saveGyToken(tok *GyToken) error {
	if err := os.MkdirAll(s.DataDir, 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(tok, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.gyTokenPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.gyTokenPath())
}

// gyRandomDeviceID 生成 32 位十六进制设备 ID。
func gyRandomDeviceID() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%032x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

// gyReadBody 读取限长响应体文本。
func gyReadBody(resp *http.Response) string {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(body)
}

// gyDecodeBody 解析响应体 JSON（失败返回 nil）。
func gyDecodeBody(resp string) map[string]any {
	var out map[string]any
	if err := json.Unmarshal([]byte(resp), &out); err != nil {
		return nil
	}
	return out
}

// gyIsSuccess 判断常见成功结构。
// 注意：部分上游接口（如 rename）操作成功但 code 为 400、msg 为 "success"。
func gyIsSuccess(decoded map[string]any) bool {
	switch v := decoded["code"].(type) {
	case float64:
		if v == 0 || v == 200 {
			return true
		}
		// 上游 rename 等接口成功时 code=400 但 msg="success"。
		if v == 400 && gyMsgIsSuccess(decoded) {
			return true
		}
		return false
	case string:
		if v == "0" || v == "200" || v == "success" {
			return true
		}
		if v == "400" && gyMsgIsSuccess(decoded) {
			return true
		}
		return false
	}
	if b, ok := decoded["success"].(bool); ok {
		return b
	}
	if b, ok := decoded["status"].(bool); ok {
		return b
	}
	// 含业务数据字段视为成功（参考客户端返回 access_token / verification_id 等）。
	if _, ok := decoded["data"]; ok {
		return true
	}
	for _, k := range []string{"access_token", "verification_id", "verification_token", "captcha_token"} {
		if v, ok := decoded[k].(string); ok && strings.TrimSpace(v) != "" {
			return true
		}
	}
	return false
}

// gyMsgIsSuccess 判断 msg/message 字段是否为 "success"。
func gyMsgIsSuccess(decoded map[string]any) bool {
	for _, k := range []string{"msg", "message"} {
		if v, ok := decoded[k].(string); ok && v == "success" {
			return true
		}
	}
	return false
}

// gyErrCode 提取错误码（业务层 error_code 或 error 字段），无错误返回空串。
func gyErrCode(decoded map[string]any) string {
	if v, ok := decoded["error_code"].(float64); ok && v != 0 {
		return "error"
	}
	if v, ok := decoded["error"].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return ""
}

// gyErrMessage 提取常见错误字段。
func gyErrMessage(decoded map[string]any, def string) string {
	for _, k := range []string{"message", "msg", "error_description", "error", "errmsg"} {
		if v, ok := decoded[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return def
}

// gyParseToken 解析 Token 响应（access_token / refresh_token / expires_in）。
func gyParseToken(resp string) (*GyToken, error) {
	decoded := gyDecodeBody(resp)
	if decoded == nil {
		return nil, fmt.Errorf("Token 响应无法解析为 JSON（%s）", truncate(resp, 120))
	}
	if !gyIsSuccess(decoded) {
		return nil, fmt.Errorf("%s", gyErrMessage(decoded, "登录接口返回失败"))
	}
	data := decoded
	if d, ok := decoded["data"].(map[string]any); ok {
		data = d
	}
	tok := &GyToken{}
	tok.AccessToken = gyStr(data, "access_token", "")
	tok.RefreshToken = gyStr(data, "refresh_token", "")
	if tok.AccessToken == "" {
		return nil, fmt.Errorf("登录响应缺少 access token（%s）", truncate(resp, 120))
	}
	if v, ok := data["expires_in"].(float64); ok && v > 0 {
		tok.ExpiresAt = time.Now().Unix() + int64(v) - 300
	}
	if v, ok := data["expires_at"].(float64); ok && v > 0 {
		tok.ExpiresAt = int64(v)
	}
	return tok, nil
}

// gyStr 安全读取 map 字符串字段。
func gyStr(m map[string]any, key, def string) string {
	if v, ok := m[key].(string); ok && strings.TrimSpace(v) != "" {
		return v
	}
	return def
}

// maskPhoneGY 手机号脱敏展示。
func maskPhoneGY(phone string) string {
	phone = strings.TrimPrefix(phone, "+86")
	if len(phone) != 11 {
		return phone
	}
	return phone[:3] + "****" + phone[7:]
}
