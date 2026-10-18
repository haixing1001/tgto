// Package service 实现 Tgto 核心业务服务：
// 配置管理、刮削整理引擎、STRM 生成、网盘转存、Telegram 频道监控与元数据发现。
package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"tgto/internal/db"
	"tgto/internal/logger"
)

// 网盘 key 常量。
const (
	Drive139    = "cloud139"
	DriveGY     = "guangya"
	DriveTianYi = "tianyi"
)

// Services 业务服务集合（整个进程单例）。
type Services struct {
	DB       *db.DB
	Log      *logger.RingLogger
	DataDir  string
	STRMRoot string
	Version  string

	limitMu  sync.RWMutex
	limiters map[string]*rate.Limiter

	sessMu   sync.RWMutex
	sessions map[string]time.Time

	httpClient interface {
		Do(req interface{}) (interface{}, error)
	}
}

// appVersion 保存当前构建版本，供共享 HTTP 客户端拼接 User-Agent。
var appVersion = "0.0.0"

// NewServices 创建服务集合。
func NewServices(database *db.DB, ring *logger.RingLogger, dataDir, strmRoot, version string) *Services {
	if version != "" {
		appVersion = version
	}
	s := &Services{
		DB:       database,
		Log:      ring,
		DataDir:  dataDir,
		STRMRoot: strmRoot,
		Version:  version,
		limiters: map[string]*rate.Limiter{},
		sessions: map[string]time.Time{},
	}
	// 关键日志（WARN/ERROR 与配置/转存/整理事件）落库。
	ring.SetPersistFunc(database.InsertSystemLog)
	return s
}

// ---------------- 配置 ----------------

// GetConfig 读取配置分区；不存在时回落默认值。
func (s *Services) GetConfig(section string) map[string]any {
	if cfg, ok := s.DB.GetConfig(section); ok {
		return cfg
	}
	if def, ok := db.DefaultConfigs()[section]; ok {
		out := map[string]any{}
		raw, _ := json.Marshal(def)
		_ = json.Unmarshal(raw, &out)
		return out
	}
	return map[string]any{}
}

// SaveConfig 合并保存配置分区（增量更新，未传字段保持原值），并将新值同步给限流器等运行时组件。
func (s *Services) SaveConfig(section string, patch map[string]any) (map[string]any, error) {
	current := s.GetConfig(section)
	for k, v := range patch {
		current[k] = v
	}
	if err := s.DB.SetConfig(section, current); err != nil {
		return nil, err
	}
	s.Log.Info(logger.ModConfig, "配置分区 %s 已保存（%d 个字段）", section, len(patch))
	s.applyRuntimeConfig(section, current)
	return current, nil
}

// AllConfigs 返回全部配置。
func (s *Services) AllConfigs() map[string]map[string]any {
	return s.DB.ListConfig()
}

// applyRuntimeConfig 配置保存后同步运行时组件（限流器等）。
func (s *Services) applyRuntimeConfig(section string, cfg map[string]any) {
	switch section {
	case Drive139, DriveGY, DriveTianYi:
		s.ConfigureLimiter(section, toFloat(cfg["qps"], 2), toFloat(cfg["burst"], 4))
	}
}

// ---------------- 会话 ----------------

// CreateSession 创建登录会话，返回 token。
func (s *Services) CreateSession(username string) string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		buf = []byte(username + time.Now().String())
	}
	token := hex.EncodeToString(buf)
	s.sessMu.Lock()
	s.sessions[token] = time.Now().Add(24 * time.Hour)
	s.sessMu.Unlock()
	return token
}

// CheckSession 校验会话有效性（滑动续期）。
func (s *Services) CheckSession(token string) bool {
	if token == "" {
		return false
	}
	s.sessMu.RLock()
	exp, ok := s.sessions[token]
	s.sessMu.RUnlock()
	if !ok || time.Now().After(exp) {
		return false
	}
	s.sessMu.Lock()
	s.sessions[token] = time.Now().Add(24 * time.Hour)
	s.sessMu.Unlock()
	return true
}

// DestroySession 注销会话。
func (s *Services) DestroySession(token string) {
	s.sessMu.Lock()
	delete(s.sessions, token)
	s.sessMu.Unlock()
}

// ---------------- 限流器 ----------------

// ConfigureLimiter 动态更新指定网盘的令牌桶（QPS + Burst）。
func (s *Services) ConfigureLimiter(drive string, qps, burst float64) {
	if qps <= 0 {
		qps = 2
	}
	if burst <= 0 {
		burst = 4
	}
	s.limitMu.Lock()
	defer s.limitMu.Unlock()
	if lim, ok := s.limiters[drive]; ok {
		lim.SetLimit(rate.Limit(qps))
		lim.SetBurst(int(burst))
		return
	}
	s.limiters[drive] = rate.NewLimiter(rate.Limit(qps), int(burst))
}

// LimiterFor 获取指定网盘的限流器。
func (s *Services) LimiterFor(drive string) *rate.Limiter {
	s.limitMu.RLock()
	lim, ok := s.limiters[drive]
	s.limitMu.RUnlock()
	if ok {
		return lim
	}
	s.ConfigureLimiter(drive, 2, 4)
	s.limitMu.RLock()
	defer s.limitMu.RUnlock()
	return s.limiters[drive]
}

// WaitLimit 阻塞等待一个令牌（带超时上下文）。
func (s *Services) WaitLimit(ctx context.Context, drive string) error {
	return s.LimiterFor(drive).Wait(ctx)
}

// ---------------- 调度器 ----------------

// StartScheduler 启动频道监控调度器。
// 轮询节奏由“全局设置-频道监控轮询节奏（分钟）”控制，
// 高频心跳日志只进内存环形缓冲，不唤醒机械硬盘。
func (s *Services) StartScheduler(ctx context.Context) {
	s.Log.Info(logger.ModScheduler, "频道监控调度器已启动")
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		lastRun := time.Time{}
		for {
			select {
			case <-ctx.Done():
				s.Log.Info(logger.ModScheduler, "频道监控调度器已停止")
				return
			case <-ticker.C:
				cfg := s.GetConfig("global_settings")
				minutes := toInt(cfg["monitor_interval"], 5)
				if minutes <= 0 {
					minutes = 5
				}
				if !lastRun.IsZero() && time.Since(lastRun) < time.Duration(minutes)*time.Minute {
					continue
				}
				lastRun = time.Now()
				s.runMonitorCycle(ctx)
			}
		}
	}()
}

// StartGyTokenRefresh 启动光鸭 Token 定时自动刷新。
// 每 10 分钟检查一次；当 token 剩余有效期不足 15 分钟时自动刷新。
func (s *Services) StartGyTokenRefresh(ctx context.Context) {
	s.Log.Info(logger.ModGuangYa, "光鸭 Token 自动刷新已启动（每 10 分钟检查，剩余 < 15 分钟时刷新）")
	go func() {
		ticker := time.NewTicker(10 * time.Minute)
		defer ticker.Stop()
		// 启动后先等 30s 再检查，避免与其他初始化竞争
		select {
		case <-ctx.Done():
			return
		case <-time.After(30 * time.Second):
		}
		for {
			select {
			case <-ctx.Done():
				s.Log.Info(logger.ModGuangYa, "光鸭 Token 自动刷新已停止")
				return
			case <-ticker.C:
				s.gyTokenRefreshCheck(ctx)
			}
		}
	}()
}

// gyTokenRefreshCheck 检查光鸭 token 是否需要刷新。
func (s *Services) gyTokenRefreshCheck(ctx context.Context) {
	tok, err := s.loadGyToken()
	if err != nil || tok == nil || tok.RefreshToken == "" {
		return // 未登录或无 refresh token，跳过
	}
	remaining := tok.ExpiresAt - time.Now().Unix()
	if remaining > 15*60 {
		return // 剩余超过 15 分钟，不需要刷新
	}
	s.Log.Info(logger.ModGuangYa, "光鸭 Token 剩余 %d 秒，触发自动刷新", remaining)
	if _, err := s.GyRefreshToken(ctx); err != nil {
		s.Log.Warn(logger.ModGuangYa, "光鸭 Token 自动刷新失败: %v", err)
	}
}

// runMonitorCycle 执行一轮频道监控（Telegram 拉取 + 心跳）。
func (s *Services) runMonitorCycle(ctx context.Context) {
	cfg := s.GetConfig("global_settings")
	token, _ := cfg["tg_bot_token"].(string)
	if token == "" {
		s.Log.Info(logger.ModScheduler, "监控心跳：未配置 Telegram Bot Token，本轮跳过远端拉取")
		return
	}
	s.Log.Info(logger.ModScheduler, "监控心跳：开始拉取 Telegram 频道更新")
	s.pollTelegram(ctx)
}

// ---------------- 类型工具 ----------------

func toInt(v any, def int) int {
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case string:
		if n, err := strconv.Atoi(t); err == nil {
			return n
		}
	}
	return def
}

func toFloat(v any, def float64) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
	}
	return def
}

func toBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1" || t == "on"
	}
	return false
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	}
	return ""
}

// toStringSlice 将配置值转换为字符串切片（支持 JSON 数组与逗号分隔字符串）。
func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			out = append(out, toString(item))
		}
		return out
	case []string:
		return t
	case string:
		if t == "" {
			return nil
		}
		parts := splitAndTrim(t, ",")
		return parts
	}
	return nil
}

func splitAndTrim(s, sep string) []string {
	out := []string{}
	start := 0
	for i := 0; i+len(sep) <= len(s); i++ {
		if s[i:i+len(sep)] == sep {
			part := trimSpace(s[start:i])
			if part != "" {
				out = append(out, part)
			}
			start = i + len(sep)
		}
	}
	if tail := trimSpace(s[start:]); tail != "" {
		out = append(out, tail)
	}
	return out
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
