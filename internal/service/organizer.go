package service

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"tgto/internal/db"
	"tgto/internal/logger"
)

// RegexRule 自定义正则强制映射规则（长篇动漫/剧集绑定 tmdbid 与 season）。
type RegexRule struct {
	Pattern string `json:"pattern"`
	TMDBID  string `json:"tmdb_id"`
	Season  int    `json:"season"`
}

// OrganizeConfig 单个网盘的整理相关配置。
type OrganizeConfig struct {
	MovieDir string `json:"movie_dir"`
	TVDir    string `json:"tv_dir"`
	MusicDir string `json:"music_dir"`
	LocalDir string `json:"local_organize_dir"`
}

// ParsedMedia 文件名解析结果。
type ParsedMedia struct {
	Type         string   `json:"type"` // movie / tv / music / unknown
	Title        string   `json:"title"`
	OriginalName string   `json:"original_name"`
	Year         int      `json:"year"`
	Season       int      `json:"season"`
	Episode      int      `json:"episode"`
	Resolution   string   `json:"resolution"`
	SourceTag    string   `json:"source_tag"`
	Codec        string   `json:"codec"`
	Audio        string   `json:"audio"`
	Group        string   `json:"group"`
	Extension    string   `json:"extension"`
	TMDBID       string   `json:"tmdb_id"`
	MBID         string   `json:"mbid"`
	Artist       string   `json:"artist"`
	Album        string   `json:"album"`
	Format       string   `json:"format"` // FLAC / WAV ...
	TrackNo      string   `json:"track_no"`
	TargetName   string   `json:"target_name"`
	TargetPath   string   `json:"target_path"`
	Notes        []string `json:"notes"`
}

var (
	reSeasonEpisode = regexp.MustCompile(`(?i)\bS(\d{1,3})[ ._-]*E(\d{1,3})\b`)
	reXxx           = regexp.MustCompile(`(?i)\b(\d{1,3})x(\d{1,3})\b`)
	reYear          = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	reResolution    = regexp.MustCompile(`(?i)\b(2160p|1080p|720p|480p|576p|4k|8k|uhd)\b`)
	reSource        = regexp.MustCompile(`(?i)\b(WEB-DL|WEBRip|BluRay|Blu-ray|BDRip|REMUX|HDTV|DVDRip|HDRip)\b`)
	reCodec         = regexp.MustCompile(`(?i)\b(HEVC|H\.?265|x265|H\.?264|x264|AVC|AV1|VC-?1)\b`)
	reAudio         = regexp.MustCompile(`(?i)\b(DTS-HD\s?MA|DTS-HD|DTS|TrueHD|Atmos|DDP?\s*5[ .]?1|DD\+?\s*5[ .]?1|EAC3|AC3|FLAC|AAC(?:\s?2[ .]?0)?|LPCM)\b`)
	reGroupTail     = regexp.MustCompile(`-([A-Za-z0-9]{2,15})$`)
	reTrackNo       = regexp.MustCompile(`^(\d{1,3})[. ]`)
	reFormatTail    = regexp.MustCompile(`(?i)\s*\[?(FLAC|WAV|APE|DSF|DFF|MP3|AAC|M4A|WMA|OGG|24BIT|16BIT|WEB)\]?\s*$`)
	reMultiSpace    = regexp.MustCompile(`\s+`)
	reSingleSong    = regexp.MustCompile(`^(?P<artist>.+?)\s+-\s+(?P<title>.+)$`)

	audioExts = map[string]string{
		".flac": "FLAC", ".wav": "WAV", ".ape": "APE", ".dsf": "DSF",
		".dff": "DFF", ".mp3": "MP3", ".aac": "AAC", ".m4a": "M4A", ".wma": "WMA", ".ogg": "OGG",
	}
	videoExts = map[string]bool{
		".mkv": true, ".mp4": true, ".avi": true, ".ts": true, ".m2ts": true,
		".iso": true, ".wmv": true, ".mov": true, ".rmvb": true, ".flv": true, ".mpg": true,
	}
)

// ParseMediaFilename 解析影视/无损音乐文件名。
// customGroups 为自定义发布组白名单（为空时不过滤），rules 为自定义正则强制映射。
func (s *Services) ParseMediaFilename(filename string, customGroups []string, rules []RegexRule) ParsedMedia {
	pm := ParsedMedia{OriginalName: filename, Type: "unknown", Notes: []string{}}

	raw := strings.TrimSpace(filename)
	raw = strings.ReplaceAll(raw, "\\", "/")
	pm.Extension = strings.ToLower(filepath.Ext(raw))

	if format, ok := audioExts[pm.Extension]; ok {
		pm.Format = format
		return s.parseMusicFilename(pm)
	}
	if !videoExts[pm.Extension] {
		pm.Type = "unknown"
		pm.Notes = append(pm.Notes, "扩展名非受支持的视频/音频格式")
		return pm
	}

	base := filepath.Base(raw)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	// 统一将点/下划线分隔替换为空格，便于按词解析
	work := strings.ReplaceAll(stem, ".", " ")
	work = strings.ReplaceAll(work, "_", " ")
	work = regexp.MustCompile(`\s+`).ReplaceAllString(work, " ")

	// 1. 季集识别：SxxExx 或 1x02
	if groups := reSeasonEpisode.FindStringSubmatch(work); groups != nil {
		pm.Type = "tv"
		pm.Season, _ = strconv.Atoi(groups[1])
		pm.Episode, _ = strconv.Atoi(groups[2])
	} else if groups := reXxx.FindStringSubmatch(work); groups != nil {
		pm.Type = "tv"
		pm.Season, _ = strconv.Atoi(groups[1])
		pm.Episode, _ = strconv.Atoi(groups[2])
	}

	// 2. 制作组（末尾 -GROUP）
	if groups := reGroupTail.FindStringSubmatch(strings.TrimSpace(stem)); groups != nil {
		pm.Group = groups[1]
	}

	// 3. 技术标签
	pm.Resolution = firstMatch(reResolution, work)
	pm.SourceTag = normalizeSource(firstMatch(reSource, work))
	pm.Codec = normalizeCodec(firstMatch(reCodec, work))
	pm.Audio = normalizeAudio(firstMatch(reAudio, work))

	// 4. 标题与年份：季集前的内容为标题主体，取其中的年份
	titlePart := work
	if loc := reSeasonEpisode.FindStringIndex(work); loc != nil {
		titlePart = work[:loc[0]]
	} else if loc := reXxx.FindStringIndex(work); loc != nil {
		titlePart = work[:loc[0]]
	}
	// 去掉制作组尾巴
	if pm.Group != "" {
		if idx := strings.LastIndex(titlePart, pm.Group); idx > 0 {
			titlePart = titlePart[:idx]
		}
	}
	if groups := reYear.FindStringSubmatch(titlePart); groups != nil {
		pm.Year, _ = strconv.Atoi(groups[1])
		if idx := strings.Index(titlePart, groups[0]); idx >= 0 {
			titlePart = titlePart[:idx]
		}
	}
	pm.Title = cleanTitle(titlePart)

	// 类型判定：有季集 → 剧集；无季集但有年份 → 电影
	if pm.Type != "tv" && pm.Year > 0 {
		pm.Type = "movie"
	}

	// 5. 自定义正则强制映射
	if rule := matchRegexRule(filename, rules); rule != nil {
		if rule.TMDBID != "" {
			pm.TMDBID = rule.TMDBID
			pm.Notes = append(pm.Notes, fmt.Sprintf("命中自定义正则规则 %s，强制绑定 tmdbid=%s", rule.Pattern, rule.TMDBID))
		}
		if rule.Season > 0 {
			pm.Season = rule.Season
			pm.Notes = append(pm.Notes, fmt.Sprintf("命中自定义正则规则 %s，强制 season=%d", rule.Pattern, rule.Season))
		}
	}

	// 6. 制作组白名单过滤
	if pm.Group != "" && len(customGroups) > 0 && !containsFold(customGroups, pm.Group) {
		pm.Notes = append(pm.Notes, fmt.Sprintf("发布组 %s 不在白名单，已从重命名结果中过滤", pm.Group))
		pm.Group = ""
	}

	// 7. 元数据匹配（TMDB key 未配置时在 API 层处理；此处保持 ID 为空则由上层补）
	return pm
}

// parseMusicFilename 解析无损音乐（专辑目录 + 曲目文件）路径。
func (s *Services) parseMusicFilename(pm ParsedMedia) ParsedMedia {
	pm.Type = "music"
	full := pm.OriginalName
	parts := strings.Split(full, "/")
	fileName := parts[len(parts)-1]
	stem := strings.TrimSuffix(fileName, filepath.Ext(fileName))

	pm.TrackNo = ""
	trackTitle := stem
	if m := reTrackNo.FindStringSubmatch(stem); m != nil {
		pm.TrackNo = m[1]
		if len(pm.TrackNo) == 1 {
			pm.TrackNo = "0" + pm.TrackNo
		}
		trackTitle = strings.TrimSpace(stem[len(m[0]):])
	}

	albumDir := ""
	if len(parts) >= 2 {
		albumDir = parts[len(parts)-2]
	}
	if albumDir != "" {
		clean := strings.ReplaceAll(albumDir, ".", " ")
		clean = reMultiSpace.ReplaceAllString(clean, " ")
		clean = strings.TrimSpace(clean)
		// 形态一：Artist - Album (Year) [Format] / Artist - Album Year Format
		if ym := reYear.FindStringIndex(clean); ym != nil {
			pm.Year, _ = strconv.Atoi(strings.TrimSpace(clean[ym[0]:ym[1]]))
			before := strings.TrimSpace(clean[:ym[0]])
			after := strings.ToUpper(clean[ym[1]:])
			if idx := strings.Index(before, " - "); idx >= 0 {
				pm.Artist = strings.TrimSpace(before[:idx])
				pm.Album = strings.TrimSpace(before[idx+3:])
			} else if idx := strings.LastIndex(before, "-"); idx > 0 {
				pm.Artist = strings.TrimSpace(before[:idx])
				pm.Album = strings.TrimSpace(before[idx+1:])
			}
			// 去掉专辑名尾部的格式标记（FLAC / WAV / 24bit 等）
			pm.Album = reFormatTail.ReplaceAllString(pm.Album, "")
			pm.Album = strings.TrimSpace(strings.Trim(pm.Album, " -–—_"))
			if strings.Contains(after, "24BIT") || strings.Contains(after, "24 BIT") {
				pm.Notes = append(pm.Notes, "检测到 24bit 高解析版本")
			}
		}
	}
	if pm.Album == "" && pm.Artist == "" {
		// 单文件"Artist - Title"形态
		clean := reMultiSpace.ReplaceAllString(strings.ReplaceAll(trackTitle, ".", " "), " ")
		clean = strings.TrimSpace(clean)
		if m := reSingleSong.FindStringSubmatch(clean); m != nil {
			pm.Artist = strings.TrimSpace(m[1])
			pm.Album = strings.TrimSpace(m[2])
			trackTitle = strings.TrimSpace(m[2])
		}
	}
	pm.Title = trackTitle
	if pm.Artist == "" {
		pm.Artist = "未知艺术家"
	}
	if pm.Album == "" {
		pm.Album = pm.Title
	}
	return pm
}

// BuildTargets 根据解析结果与整理配置生成标准重命名目标（目录 + 文件名）。
func (s *Services) BuildTargets(pm *ParsedMedia, cfg OrganizeConfig) {
	quality := strings.Join(nonEmpty(pm.Resolution, pm.SourceTag, pm.Codec), " ")
	switch pm.Type {
	case "tv":
		season := pm.Season
		if season <= 0 {
			season = 1
			pm.Notes = append(pm.Notes, "未识别到季号，默认按 Season 01 处理")
		}
		dirName := tmdbFolder(pm.Title, pm.Year, pm.TMDBID)
		base := fmt.Sprintf("%s - S%02dE%02d", shorten(pm.Title), season, pm.Episode)
		if quality != "" {
			base += " - " + quality
		}
		pm.TargetName = base + pm.Extension
		pm.TargetPath = filepath.Join(cfg.TVDir, dirName, fmt.Sprintf("Season %02d", season), pm.TargetName)
	case "movie":
		dirName := tmdbFolder(pm.Title, pm.Year, pm.TMDBID)
		base := shorten(pm.Title)
		if pm.Year > 0 {
			base = fmt.Sprintf("%s (%d)", base, pm.Year)
		}
		if quality != "" {
			base += " - " + quality
		}
		pm.TargetName = base + pm.Extension
		pm.TargetPath = filepath.Join(cfg.MovieDir, dirName, pm.TargetName)
	case "music":
		dirName := fmt.Sprintf("%s - %s (%d) [%s]", pm.Artist, pm.Album, pm.Year, pm.Format)
		pm.TargetName = fmt.Sprintf("%s - %s - %s%s", orDefault(pm.TrackNo, "01"), pm.Artist, pm.Title, pm.Extension)
		pm.TargetPath = filepath.Join(cfg.MusicDir, dirName, pm.TargetName)
	default:
		pm.Notes = append(pm.Notes, "无法识别的文件名类型")
	}
}

// TestParse 文件名识别测试沙盒使用的入口（组合解析 + 目标生成）。
func (s *Services) TestParse(filename string) (ParsedMedia, OrganizeConfig, error) {
	tools := s.GetConfig("tools")
	groups := toStringSlice(tools["custom_groups"])
	rules := parseRegexRules(tools["regex_rules"])
	pm := s.ParseMediaFilename(filename, groups, rules)
	cfg := OrganizeConfig{MovieDir: "/媒体库/电影", TVDir: "/媒体库/剧集", MusicDir: "/媒体库/无损音乐"}
	s.BuildTargets(&pm, cfg)
	return pm, cfg, nil
}

// OrganizeRun 触发指定网盘的盘内整理：扫描本地整理目录 + 处理整理队列，写入整理历史。
func (s *Services) OrganizeRun(drive string) (int, int, error) {
	cfg := s.GetConfig(drive)
	organize := OrganizeConfig{
		MovieDir: orDefault(toString(cfg["movie_dir"]), "/媒体库/电影"),
		TVDir:    orDefault(toString(cfg["tv_dir"]), "/媒体库/剧集"),
		MusicDir: orDefault(toString(cfg["music_dir"]), "/媒体库/无损音乐"),
		LocalDir: orDefault(toString(cfg["local_organize_dir"]), s.DataDir+"/organize/"+drive),
	}
	tools := s.GetConfig("tools")
	groups := toStringSlice(tools["custom_groups"])
	rules := parseRegexRules(tools["regex_rules"])

	success, failed := 0, 0
	record := func(pm ParsedMedia, errMsg string) {
		row := db.OrganizeRow{
			Drive:        drive,
			Kind:         kindOf(pm),
			OriginalName: pm.OriginalName,
			TargetName:   pm.TargetName,
			TargetPath:   pm.TargetPath,
			TMDBID:       pm.TMDBID,
			MBID:         pm.MBID,
		}
		if errMsg != "" {
			row.Status = "失败"
			row.Error = errMsg
			failed++
			s.Log.Warn(logger.ModOrganizer, "[%s] 整理失败: %s（%s）", drive, pm.OriginalName, errMsg)
		} else {
			row.Status = "成功"
			success++
			s.Log.Info(logger.ModOrganizer, "[%s] 整理完成: %s -> %s", drive, pm.OriginalName, pm.TargetPath)
		}
		if _, err := s.DB.InsertOrganize(&row); err != nil {
			s.Log.Error(logger.ModOrganizer, "写入整理历史失败: %v", err)
		}
	}

	// 1. 扫描本地整理目录中的真实文件
	_ = filepath.Walk(organize.LocalDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(organize.LocalDir, path)
		if relErr != nil {
			return nil
		}
		pm := s.ParseMediaFilename(filepath.ToSlash(rel), groups, rules)
		if pm.Type == "unknown" {
			record(pm, "无法识别的文件名类型")
			return nil
		}
		s.BuildTargets(&pm, organize)
		record(pm, "")
		return nil
	})

	// 2. 处理整理队列（UI 演示与频道监控入队）
	queue := s.GetConfig("organize_queue")
	rawQueue, _ := queue[drive].([]any)
	for _, item := range rawQueue {
		name := toString(item)
		if name == "" {
			continue
		}
		pm := s.ParseMediaFilename(name, groups, rules)
		if pm.Type == "unknown" {
			record(pm, "无法识别的文件名类型")
			continue
		}
		s.BuildTargets(&pm, organize)
		record(pm, "")
	}
	if len(rawQueue) > 0 {
		queue[drive] = []any{}
		_ = s.DB.SetConfig("organize_queue", queue)
	}

	s.Log.Info(logger.ModOrganizer, "[%s] 盘内整理任务结束：成功 %d，失败 %d", drive, success, failed)
	return success, failed, nil
}

// ---------------- 内部工具 ----------------

func kindOf(pm ParsedMedia) string {
	switch pm.Type {
	case "tv", "movie":
		return pm.Type
	case "music":
		return "music"
	}
	return "movie"
}

func tmdbFolder(title string, year int, tmdbID string) string {
	if tmdbID != "" {
		return fmt.Sprintf("%s (%d) [tmdbid=%s]", shorten(title), year, tmdbID)
	}
	return fmt.Sprintf("%s (%d)", shorten(title), year)
}

func firstMatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func normalizeSource(v string) string {
	if v == "" {
		return ""
	}
	switch strings.ToUpper(v) {
	case "BLU-RAY":
		return "BluRay"
	case "TRUEHD":
		return ""
	}
	return v
}

func normalizeCodec(v string) string {
	if v == "" {
		return ""
	}
	up := strings.ToUpper(strings.ReplaceAll(v, ".", ""))
	switch {
	case strings.Contains(up, "265"), strings.Contains(up, "HEVC"):
		return "HEVC"
	case strings.Contains(up, "264"), strings.Contains(up, "AVC"):
		return "H264"
	case strings.Contains(up, "AV1"):
		return "AV1"
	}
	return v
}

func normalizeAudio(v string) string {
	if v == "" {
		return ""
	}
	up := strings.ToUpper(v)
	up = regexp.MustCompile(`\s+`).ReplaceAllString(up, "")
	switch {
	case strings.Contains(up, "DTS-HDMA"):
		return "DTS-HD MA"
	case strings.Contains(up, "DTS-HD"):
		return "DTS-HD"
	case strings.Contains(up, "DTS"):
		return "DTS"
	case strings.Contains(up, "TRUEHD"):
		return "TrueHD"
	case strings.Contains(up, "ATMOS"):
		return "Atmos"
	case strings.Contains(up, "DDP"), strings.Contains(up, "DD+"):
		return "DDP5.1"
	case strings.Contains(up, "EAC3"):
		return "EAC3"
	case strings.Contains(up, "AC3"):
		return "AC3"
	case strings.Contains(up, "FLAC"):
		return "FLAC"
	case strings.Contains(up, "AAC"):
		return "AAC"
	}
	return v
}

func cleanTitle(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, " -–—_|/")
	fields := strings.Fields(s)
	return strings.Join(fields, " ")
}

func shorten(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "未知标题"
	}
	return s
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func nonEmpty(vals ...string) []string {
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			out = append(out, v)
		}
	}
	return out
}

func containsFold(list []string, v string) bool {
	for _, item := range list {
		if strings.EqualFold(strings.TrimSpace(item), strings.TrimSpace(v)) {
			return true
		}
	}
	return false
}

// parseRegexRules 解析配置中的自定义正则规则列表。
func parseRegexRules(v any) []RegexRule {
	items, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]RegexRule, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		out = append(out, RegexRule{
			Pattern: toString(m["pattern"]),
			TMDBID:  toString(m["tmdb_id"]),
			Season:  toInt(m["season"], 0),
		})
	}
	return out
}

// matchRegexRule 查找首个匹配文件名（或其基础名）的正则规则。
func matchRegexRule(filename string, rules []RegexRule) *RegexRule {
	base := filepath.Base(filename)
	for i := range rules {
		if rules[i].Pattern == "" {
			continue
		}
		re, err := regexp.Compile(rules[i].Pattern)
		if err != nil {
			continue
		}
		if re.MatchString(filename) || re.MatchString(base) {
			return &rules[i]
		}
	}
	return nil
}
