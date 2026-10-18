package service

import (
	"context"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"tgto/internal/db"
	"tgto/internal/logger"
)

// ShareLink 识别后的分享链接信息。
type ShareLink struct {
	Raw     string `json:"raw"`
	URL     string `json:"url"`
	Code    string `json:"code"`
	Drive   string `json:"drive"` // cloud139 / guangya / tianyi
	ShareID string `json:"share_id"`
	Host    string `json:"host"`
}

// 常见链接写法：域名/s/xxxxx、域名/s/xxxxx#提取码、链接 + 空格 + 提取码
var (
	rePan139  = regexp.MustCompile(`(?i)(yun\.139\.com|caiyun\.139\.com|139\.com)`)
	rePanGY   = regexp.MustCompile(`(?i)(guangya|gygd|guanying)`)
	rePanTY   = regexp.MustCompile(`(?i)(cloud\.189\.cn|189\.cn|tianyi)`)
	reAnyURL  = regexp.MustCompile(`https?://[^\s"'<>]+`)
	reShareID = regexp.MustCompile(`(?i)/(?:s|t)/([A-Za-z0-9_-]+)`)
	// 移动云盘分享：yun.139.com/shareweb/#/w/i/<linkID>、/w/i/<linkID>、?linkID=<id>
	reShareID139 = regexp.MustCompile(`(?i)(?:/w/i/|linkid=)([A-Za-z0-9_-]{4,})`)
)

// ParseShareLink 识别四大网盘分享链接并提取分享 ID 与提取码。
func ParseShareLink(raw string) (*ShareLink, error) {
	input := strings.TrimSpace(raw)
	if input == "" {
		return nil, fmt.Errorf("分享链接不能为空")
	}

	// 兼容 "链接 提取码" / "链接#提取码" / "链接+提取码" 三种写法。
	// 注意顺序：先按 URL 切分，再考虑 '#'——移动云盘的分享 ID 就写在 #/w/i/xxx 里，
	// 若先按 '#' 切分会把整段路由当成提取码。
	code := ""
	linkPart := input
	if m := reAnyURL.FindString(input); m != "" {
		rest := strings.TrimSpace(strings.TrimPrefix(input, m))
		if rest != "" {
			code = strings.TrimSpace(strings.TrimLeft(rest, " +-＋－提取码:：#＃"))
		}
		linkPart = m
	} else if idx := strings.IndexAny(input, "#＃"); idx >= 0 {
		code = strings.TrimSpace(input[idx+1:])
		linkPart = input[:idx]
	}

	linkPart = strings.TrimSpace(linkPart)
	if !strings.HasPrefix(strings.ToLower(linkPart), "http") {
		linkPart = "https://" + strings.TrimLeft(linkPart, "/")
	}
	parsed, err := url.Parse(linkPart)
	if err != nil || parsed.Hostname() == "" {
		return nil, fmt.Errorf("无法解析分享链接: %s", raw)
	}
	host := strings.ToLower(parsed.Hostname())

	link := &ShareLink{Raw: raw, URL: parsed.String(), Host: host}
	// 查询参数中的提取码优先
	if q := parsed.Query().Get("code"); q != "" && code == "" {
		code = q
	}
	// 注意：移动云盘把分享 ID 放在 fragment（#/w/i/xxx），不能当成提取码
	if frag := parsed.Fragment; frag != "" && code == "" && !strings.Contains(frag, "/") && len(frag) <= 16 {
		code = frag
	}
	link.Code = code

	shareID := ""
	if m := reShareID.FindStringSubmatch(parsed.Path); m != nil {
		shareID = m[1]
	}
	if shareID == "" {
		// 移动云盘：分享 ID 可能在 fragment 或 linkID 查询参数里
		if m := reShareID139.FindStringSubmatch(parsed.Fragment); m != nil {
			shareID = m[1]
		} else if id := parsed.Query().Get("linkID"); id != "" {
			shareID = id
		} else if m := reShareID139.FindStringSubmatch(parsed.String()); m != nil {
			shareID = m[1]
		}
	}
	link.ShareID = shareID

	switch {
	case rePan139.MatchString(host):
		link.Drive = Drive139
	case rePanGY.MatchString(host):
		link.Drive = DriveGY
	case rePanTY.MatchString(host):
		link.Drive = DriveTianYi
	default:
		return nil, fmt.Errorf("无法识别网盘类型（支持 移动云盘 / 光鸭云盘 / 天翼云盘）: %s", host)
	}
	if shareID == "" {
		return nil, fmt.Errorf("链接中未找到分享 ID: %s", parsed.String())
	}
	return link, nil
}

// TransferSubmitRequest 转存提交请求。
type TransferSubmitRequest struct {
	Link      string `json:"link"`
	Code      string `json:"code"`
	TargetDir string `json:"target_dir"`
	Channel   string `json:"channel"`
}

// SubmitTransfer 提交分享链接转存任务：识别网盘 -> 去重 -> 限流 -> 写入历史。
func (s *Services) SubmitTransfer(ctx context.Context, req TransferSubmitRequest) (*db.TransferRow, error) {
	full := strings.TrimSpace(req.Link)
	if req.Code != "" && !strings.Contains(full, req.Code) {
		full = full + "#" + strings.TrimSpace(req.Code)
	}
	link, err := ParseShareLink(full)
	if err != nil {
		return nil, err
	}

	// 去重：同一链接 + 同一目标目录已成功过则跳过
	target := orDefault(req.TargetDir, s.defaultTarget(link.Drive))
	if exist, ok := s.DB.FindTransferByLink(link.URL, target); ok {
		exist.DriveName = db.DriveNames[link.Drive]
		exist.Category = link.Drive
		exist.Result = "已去重"
		s.Log.Info(logger.ModTransfer, "链接 %s 已存在于转存历史（#%d），执行去重跳过", link.URL, exist.ID)
		return exist, nil
	}

	// 限流（令牌桶）
	if err := s.WaitLimit(ctx, link.Drive); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}

	row := &db.TransferRow{
		Drive:     link.Drive,
		Channel:   orDefault(req.Channel, "手动提交"),
		ShareLink: link.URL,
		Code:      link.Code,
		TargetDir: target,
		Status:    "成功",
		Message:   fmt.Sprintf("识别为%s分享 %s，沙盒模式：已完成链接校验与本地登记（配置云盘凭证后将执行远端转存）", driveDisplayName(link.Drive), link.ShareID),
		Result:    "已登记",
	}
	// 移动云盘：登录后执行真实转存（其余网盘仍是登记模式）
	if link.Drive == Drive139 && s.y139LoggedIn() {
		msg, terr := s.Yun139Transfer(ctx, link.ShareID, link.Code, target)
		if terr != nil {
			row.Status = "失败"
			row.Result = "失败"
			row.Message = "移动云盘转存失败: " + terr.Error()
		} else {
			row.Status = "成功"
			row.Result = "已转存"
			row.Message = msg
		}
	}
	if _, err := s.DB.InsertTransfer(row); err != nil {
		s.Log.Error(logger.ModTransfer, "写入转存历史失败: %v", err)
		return nil, err
	}
	row.DriveName = db.DriveNames[link.Drive]
	row.Category = link.Drive
	s.Log.Info(logger.ModTransfer, "[%s] 转存任务已登记: %s -> %s", db.DriveNames[link.Drive], link.URL, target)
	return row, nil
}

// defaultTarget 返回各网盘默认转存目录。
func (s *Services) defaultTarget(drive string) string {
	cfg := s.GetConfig(drive)
	switch drive {
	case DriveTianYi, DriveGY:
		return orDefault(toString(cfg["target_dir"]), "/媒体库")
	default:
		return orDefault(toString(cfg["movie_dir"]), "/媒体库/电影")
	}
}

// pollTelegram 通过 Bot API 拉取频道更新，提取分享链接并提交转存。
func (s *Services) pollTelegram(ctx context.Context) {
	cfg := s.GetConfig("global_settings")
	token := toString(cfg["tg_bot_token"])
	if token == "" {
		return
	}
	channels := toStringSlice(cfg["monitor_channels"])

	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/getUpdates?timeout=0&allowed_updates=[\"channel_post\"]", token)
	body, err := httpGetText(ctx, endpoint)
	if err != nil {
		s.Log.Warn(logger.ModScheduler, "Telegram getUpdates 失败: %v", err)
		return
	}

	updates := parseTelegramUpdates(body)
	matched, submitted := 0, 0
	for _, u := range updates {
		if len(channels) > 0 && u.ChannelTitle != "" && !containsFold(channels, u.ChannelTitle) {
			continue
		}
		matched++
		for _, rawURL := range reAnyURL.FindAllString(u.Text, -1) {
			link, parseErr := ParseShareLink(rawURL)
			if parseErr != nil {
				continue
			}
			row, subErr := s.SubmitTransfer(ctx, TransferSubmitRequest{
				Link:    link.Raw,
				Channel: u.ChannelTitle,
			})
			if subErr != nil {
				s.Log.Warn(logger.ModTransfer, "频道 %s 链接转存失败: %v", u.ChannelTitle, subErr)
				continue
			}
			submitted++
			_ = row
		}
	}
	if matched == 0 {
		s.Log.Info(logger.ModScheduler, "本轮未拉到匹配监控频道的消息（监控 %d 个频道）", len(channels))
		return
	}
	s.Log.Info(logger.ModScheduler, "本轮处理频道消息 %d 条，提交转存 %d 个", matched, submitted)
}

// SendTelegramMessage 发送 Telegram 测试消息。
func (s *Services) SendTelegramMessage(ctx context.Context, text string) error {
	cfg := s.GetConfig("global_settings")
	token := toString(cfg["tg_bot_token"])
	chatID := toString(cfg["tg_user_id"])
	if token == "" || chatID == "" {
		return fmt.Errorf("请先配置主机器人 Bot Token 与你的 TG 用户 ID")
	}
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", token)
	form := url.Values{}
	form.Set("chat_id", chatID)
	form.Set("text", text)
	form.Set("parse_mode", "HTML")
	resp, err := httpPostForm(ctx, endpoint, form)
	if err != nil {
		return err
	}
	if !strings.Contains(resp, `"ok":true`) {
		return fmt.Errorf("Telegram 返回异常: %s", truncate(resp, 200))
	}
	s.Log.Info(logger.ModSystem, "Telegram 测试消息已发送至 %s", chatID)
	return nil
}

// TestProxy 测试网络代理延迟。
func (s *Services) TestProxy(ctx context.Context, proxyURL string) (map[string]any, error) {
	proxyURL = strings.TrimSpace(proxyURL)
	if proxyURL == "" {
		return nil, fmt.Errorf("请先填写代理地址")
	}
	started := time.Now()
	body, err := httpGetViaProxy(ctx, proxyURL, "https://api.themoviedb.org/3/configuration")
	latency := time.Since(started).Milliseconds()
	if err != nil {
		s.Log.Warn(logger.ModSystem, "代理测试失败: %v", err)
		return map[string]any{"ok": false, "latency_ms": latency, "error": err.Error()}, nil
	}
	ok := strings.Contains(body, "images")
	s.Log.Info(logger.ModSystem, "代理测试完成: %s 延迟 %dms", proxyURL, latency)
	return map[string]any{"ok": ok, "latency_ms": latency, "detail": truncate(body, 300)}, nil
}

// TestTMDBKey 测试 TMDB API Key 连通性。
func (s *Services) TestTMDBKey(ctx context.Context) (map[string]any, error) {
	cfg := s.GetConfig("global_settings")
	key := toString(cfg["tmdb_api_key"])
	if key == "" {
		return nil, fmt.Errorf("请先填写 TMDB API Key")
	}
	tools := s.GetConfig("tools")
	base := orDefault(toString(tools["tmdb_api_base"]), "https://api.themoviedb.org")
	body, err := httpGetText(ctx, fmt.Sprintf("%s/3/configuration?api_key=%s", strings.TrimRight(base, "/"), url.QueryEscape(key)))
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}, nil
	}
	ok := strings.Contains(body, "images")
	return map[string]any{"ok": ok, "detail": truncate(body, 200)}, nil
}

// telegramUpdate 解析后的 Telegram 频道消息。
type telegramUpdate struct {
	ChannelTitle string
	Text         string
}

// parseTelegramUpdates 极简解析 getUpdates 响应中的 channel_post 消息。
func parseTelegramUpdates(body string) []telegramUpdate {
	out := []telegramUpdate{}
	// 结构: {"ok":true,"result":[{"update_id":..,"channel_post":{"message_id":..,"chat":{"title":".."},"text":".."}}]}
	if !strings.Contains(body, "channel_post") {
		return out
	}
	texts := regexp.MustCompile(`"text"\s*:\s*"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(body, -1)
	titles := regexp.MustCompile(`"title"\s*:\s*"((?:[^"\\]|\\.)*)"`).FindAllStringSubmatch(body, -1)
	for i, t := range texts {
		title := ""
		if i < len(titles) {
			title = unescapeJSON(titles[i][1])
		}
		out = append(out, telegramUpdate{ChannelTitle: title, Text: unescapeJSON(t[1])})
	}
	return out
}

func unescapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\/`, `/`)
	s = strings.ReplaceAll(s, `\"`, `"`)
	s = strings.ReplaceAll(s, `\n`, "\n")
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
