// Package db 负责 SQLite（modernc 纯 Go 驱动，无 CGO）初始化、
// WAL 模式配置、核心表结构与默认种子数据。
//
// 针对小型 NAS 机械硬盘的优化：
//   - journal_mode=WAL + synchronous=NORMAL：大幅降低随机写放大；
//   - temp_store=MEMORY：临时表/索引不进磁盘；
//   - cache_size=-4000：限制页缓存约 4MB，控制常驻内存。
package db

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"tgto/internal/logger"
)

// DB 数据库句柄封装。
type DB struct {
	SQL *sql.DB
}

// TransferRow 转存历史行。
type TransferRow struct {
	ID         int64  `json:"id"`
	Drive      string `json:"drive"`
	Channel    string `json:"channel"`
	ShareLink  string `json:"share_link"`
	Code       string `json:"code"`
	TargetDir  string `json:"target_dir"`
	Status     string `json:"status"`
	Message    string `json:"message"`
	Result     string `json:"result"`
	ShareURL   string `json:"share_url"`
	DriveName  string `json:"drive_name"`
	Category   string `json:"category"`
	Original   string `json:"original_name"`
	CreatedAt  string `json:"created_at"`
}

// OrganizeRow 整理历史行。
type OrganizeRow struct {
	ID           int64  `json:"id"`
	Drive        string `json:"drive"`
	Kind         string `json:"kind"` // movie / tv / music / share
	OriginalName string `json:"original_name"`
	TargetName   string `json:"target_name"`
	TargetPath   string `json:"target_path"`
	TMDBID       string `json:"tmdb_id"`
	MBID         string `json:"mbid"`
	Status       string `json:"status"` // 成功 / 失败 / 已回退
	Error        string `json:"error"`
	CreatedAt    string `json:"created_at"`
}

// DriveNames 网盘 key -> 中文展示名。
var DriveNames = map[string]string{
	"cloud139":  "移动云盘",
	"cloud115":  "115网盘（历史数据）",
	"cloud123":  "123云盘（已下线）",
	"guangya":   "光鸭云盘",
	"tianyi":    "天翼云盘",
}

// Open 打开（必要时创建）SQLite 数据库并完成建表与种子数据写入。
func Open(dir string, ring *logger.RingLogger) (*DB, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("创建数据库目录失败: %w", err)
	}
	dbPath := filepath.Join(dir, "Tgto.db")

	sqlDB, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		return nil, fmt.Errorf("打开 SQLite 失败: %w", err)
	}
	sqlDB.SetMaxOpenConns(4)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(time.Hour)

	for _, pragma := range []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA temp_store=MEMORY;",
		"PRAGMA cache_size=-4000;",
		"PRAGMA busy_timeout=5000;",
	} {
		if _, err := sqlDB.Exec(pragma); err != nil {
			sqlDB.Close()
			return nil, fmt.Errorf("执行 %s 失败: %w", pragma, err)
		}
	}

	d := &DB{SQL: sqlDB}
	if err := d.migrate(); err != nil {
		return nil, err
	}
	if err := d.seed(); err != nil {
		return nil, err
	}
	ring.Info(logger.ModSystem, "SQLite 初始化完成: %s（WAL / synchronous=NORMAL / cache=-4000KB）", dbPath)
	return d, nil
}

// Close 关闭数据库。
func (d *DB) Close() error {
	if d == nil || d.SQL == nil {
		return nil
	}
	return d.SQL.Close()
}

func (d *DB) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS kv_config (
			section    TEXT PRIMARY KEY,
			data       TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS transfer_history (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			drive      TEXT NOT NULL DEFAULT '',
			channel    TEXT NOT NULL DEFAULT '',
			share_link TEXT NOT NULL DEFAULT '',
			code       TEXT NOT NULL DEFAULT '',
			target_dir TEXT NOT NULL DEFAULT '',
			status     TEXT NOT NULL DEFAULT '成功',
			message    TEXT NOT NULL DEFAULT '',
			result     TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_transfer_drive  ON transfer_history(drive);`,
		`CREATE INDEX IF NOT EXISTS idx_transfer_status ON transfer_history(status);`,
		`CREATE TABLE IF NOT EXISTS organize_history (
			id            INTEGER PRIMARY KEY AUTOINCREMENT,
			drive         TEXT NOT NULL DEFAULT '',
			kind          TEXT NOT NULL DEFAULT 'movie',
			original_name TEXT NOT NULL DEFAULT '',
			target_name   TEXT NOT NULL DEFAULT '',
			target_path   TEXT NOT NULL DEFAULT '',
			tmdb_id       TEXT NOT NULL DEFAULT '',
			mbid          TEXT NOT NULL DEFAULT '',
			status        TEXT NOT NULL DEFAULT '成功',
			error         TEXT NOT NULL DEFAULT '',
			created_at    TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_organize_drive ON organize_history(drive, kind);`,
		`CREATE INDEX IF NOT EXISTS idx_organize_st    ON organize_history(status);`,
		`CREATE TABLE IF NOT EXISTS system_logs (
			id         INTEGER PRIMARY KEY AUTOINCREMENT,
			level      TEXT NOT NULL DEFAULT 'INFO',
			module     TEXT NOT NULL DEFAULT '',
			message    TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_logs_created ON system_logs(created_at);`,
	}
	for _, s := range stmts {
		if _, err := d.SQL.Exec(s); err != nil {
			return fmt.Errorf("建表失败: %w", err)
		}
	}
	return nil
}

// DefaultConfigs 返回全部配置分区的默认值（与前端结构对齐）。
func DefaultConfigs() map[string]map[string]any {
	return map[string]map[string]any{
		"global_settings": {
			"proxy":               "",
			"tmdb_api_key":        "",
			"tg_bot_token":        "",
			"tg_user_id":          "",
			"monitor_interval":    5,
			"metadata_language":   "zh-CN",
		},
		"cloud139": {
			"username":          "",
			"password":          "",
			"mail_cookies":      "",
			"authorization":     "",
			"monitor_channels":  "",
			"organize_enabled":  true,
			"movie_dir":         "/媒体库/电影",
			"tv_dir":            "/媒体库/剧集",
			"music_dir":         "/媒体库/无损音乐",
			"local_organize_dir": "/app/data/organize/cloud139",
			"local_media_dir":    "/app/data/media/cloud139",
			"strm_enabled":      true,
			"strm_dir":          "/app/strm/cloud139",
			"strm_url_prefix":   "http://127.0.0.1:9058/media/cloud139",
			"share_strm_enabled": false,
			"qps":                2,
			"burst":              4,
		},
		"guangya": {
			"phone":            "",
			"account":          "",
			"password":         "",
			"monitor_channels":  "",
			"transfer_target":   "/媒体库/转存",
			"rapid_json":        "",
			"pp_enabled":        false,
			"xunlei_enabled":    false,
			"offline_enabled":   false,
			"extract_enabled":   false,
			"organize_enabled":  true,
			"movie_dir":         "/媒体库/电影",
			"tv_dir":            "/媒体库/剧集",
			"music_dir":         "/媒体库/无损音乐",
			"local_organize_dir": "/app/data/organize/guangya",
			"local_media_dir":    "/app/data/media/guangya",
			"strm_enabled":      true,
			"strm_dir":          "/app/strm/guangya",
			"strm_url_prefix":   "http://127.0.0.1:9058/media/guangya",
			"qps":                2,
			"burst":              4,
		},
		"tianyi": {
			"account":          "",
			"password":         "",
			"monitor_channels": "",
			"target_dir":       "/媒体库",
		},
		"ui": {
			"theme": "dark",
		},
		"tools": {
			"command_alias":   "/tr=/transfer\n/ls=/list",
			"ai_base_url":     "",
			"ai_model":        "gpt-4o-mini",
			"ai_api_key":      "",
			"custom_groups":   "FRDS, CMCT, WiKi, CHD, HHWEB",
			"tmdb_api_base":   "https://api.themoviedb.org",
			"tmdb_image_base": "https://image.tmdb.org",
			"regex_rules":     []any{},
			"share_strm_path": "/app/strm/cloud139_share",
		},
		"basic_config": {
			"douban_tiers":      []any{"热门电影", "豆瓣电影Top250", "华语佳片", "冷门佳作"},
			"proxy_nodes":       []any{},
			"reo_account":       "",
			"guanying_enabled":  false,
			"guanying_user":     "",
			"guanying_password": "",
		},
		"organize_queue": {
			"guangya": []any{
				"繁花.2023.S01E02.1080p.WEB-DL.H264.AAC-HDS.mkv",
				"陈奕迅.-.Rice.And.Shine.2014.FLAC/05.娱乐天空.flac",
			},
			"cloud139": []any{
				"漫长的季节.2023.S01E04.2160p.WEB-DL.DDP5.1.Atmos.H.265-HHWEB.mkv",
				"周杰伦.-.叶惠美.2003.FLAC/03.晴天.flac",
				"幕府将军.2024.S01E01.1080p.AMZN.WEB-DL.DDP5.1.H.264-FRDS.mkv",
			},
		},
	}
}

// seed 写入默认配置与初始历史种子数据（仅首次）。
func (d *DB) seed() error {
	for section, cfg := range DefaultConfigs() {
		var count int
		if err := d.SQL.QueryRow(`SELECT COUNT(1) FROM kv_config WHERE section = ?`, section).Scan(&count); err != nil {
			return err
		}
		if count == 0 {
			if err := d.SetConfig(section, cfg); err != nil {
				return err
			}
		}
	}

	var transferCount, organizeCount int
	_ = d.SQL.QueryRow(`SELECT COUNT(1) FROM transfer_history`).Scan(&transferCount)
	_ = d.SQL.QueryRow(`SELECT COUNT(1) FROM organize_history`).Scan(&organizeCount)

	now := time.Now()
	if transferCount == 0 {
		seeds := []TransferRow{
			{Drive: "cloud139", Channel: "@PTShare", ShareLink: "https://yun.139.com/shareweb/#/w/i/2qieM8ZkLpQ", Code: "", TargetDir: "/媒体库/电影", Status: "成功", Message: "转存成功，共 3 个文件", Result: "已入库"},
			{Drive: "cloud139", Channel: "@LosslessMusic", ShareLink: "https://yun.139.com/shareweb/#/w/i/2qieN5tRbVc", Code: "k5tt", TargetDir: "/媒体库/无损音乐", Status: "成功", Message: "转存成功，共 1 个目录", Result: "已入库"},
			{Drive: "guangya", Channel: "@GygdShare", ShareLink: "https://guangya.com/s/gy7k9m2x", Code: "gg22", TargetDir: "/媒体库/转存", Status: "跳过", Message: "分享链接已存在于监控历史，执行去重跳过", Result: "已去重"},
			{Drive: "tianyi", Channel: "@Cloud189", ShareLink: "https://cloud.189.cn/t/eyR3mN7A", Code: "6f2q", TargetDir: "/媒体库/剧集", Status: "失败", Message: "天翼云盘分享已过期（code: ShareExpired）", Result: "链接失效"},
		}
		for i := range seeds {
			seeds[i].CreatedAt = now.Add(-time.Duration(len(seeds)-i) * 37 * time.Minute).Format(time.RFC3339)
			if _, err := d.InsertTransfer(&seeds[i]); err != nil {
				return err
			}
		}
	}

	if organizeCount == 0 {
		seeds := []OrganizeRow{
			{Drive: "guangya", Kind: "movie", OriginalName: "繁花.2023.S01E02.1080p.WEB-DL.H264.AAC-HDS.mkv", TargetName: "繁华 (2023) - S01E02 - 1080p WEB-DL H264.mkv", TargetPath: "/媒体库/剧集/繁华 (2023) [tmdbid=225372]/Season 01/繁华 (2023) - S01E02 - 1080p WEB-DL H264.mkv", TMDBID: "225372", Status: "成功"},
			{Drive: "guangya", Kind: "music", OriginalName: "陈奕迅.-.Rice.And.Shine.2014.FLAC/05.娱乐天空.flac", TargetName: "05 - 陈奕迅 - 娱乐天空.flac", TargetPath: "/媒体库/无损音乐/陈奕迅 - Rice And Shine (2014) [FLAC]/05 - 陈奕迅 - 娱乐天空.flac", MBID: "c3d4e5f6-a7b8-4c9d-8e0f-1a2b3c4d5e6f", Status: "成功"},
			{Drive: "cloud139", Kind: "movie", OriginalName: "漫长的季节.2023.S01E04.2160p.WEB-DL.DDP5.1.Atmos.H.265-HHWEB.mkv", TargetName: "漫长的季节 (2023) - S01E04 - 2160p WEB-DL H265.mkv", TargetPath: "/媒体库/剧集/漫长的季节 (2023) [tmdbid=225372]/Season 01/漫长的季节 (2023) - S01E04 - 2160p WEB-DL H265.mkv", TMDBID: "225372", Status: "成功"},
			{Drive: "cloud139", Kind: "music", OriginalName: "周杰伦.-.叶惠美.2003.FLAC/03.晴天.flac", TargetName: "03 - 周杰伦 - 晴天.flac", TargetPath: "/媒体库/无损音乐/周杰伦 - 叶惠美 (2003) [FLAC]/03 - 周杰伦 - 晴天.flac", MBID: "d4e5f6a7-b8c9-4d0e-9f1a-2b3c4d5e6f70", Status: "成功"},
			{Drive: "cloud139", Kind: "share", OriginalName: "幕府将军.2024.S01E01.1080p.AMZN.WEB-DL.DDP5.1.H.264-FRDS.mkv", TargetName: "幕府将军 (2024) - S01E01 - 1080p WEB-DL H264.strm", TargetPath: "/app/strm/cloud139_share/幕府将军 (2024) [tmdbid=126308]/Season 01/幕府将军 (2024) - S01E01.strm", TMDBID: "126308", Status: "已回退"},
		}
		for i := range seeds {
			seeds[i].CreatedAt = now.Add(-time.Duration(len(seeds)-i) * 53 * time.Minute).Format(time.RFC3339)
			if _, err := d.InsertOrganize(&seeds[i]); err != nil {
				return err
			}
		}
	}
	return nil
}

// ---------------- kv_config ----------------

// GetConfig 读取配置分区；不存在时返回 nil, false。
func (d *DB) GetConfig(section string) (map[string]any, bool) {
	var raw string
	err := d.SQL.QueryRow(`SELECT data FROM kv_config WHERE section = ?`, section).Scan(&raw)
	if err != nil {
		return nil, false
	}
	out := map[string]any{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, false
	}
	return out, true
}

// SetConfig 覆盖写入配置分区。
func (d *DB) SetConfig(section string, data map[string]any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = d.SQL.Exec(
		`INSERT INTO kv_config(section, data, updated_at) VALUES(?,?,?)
		 ON CONFLICT(section) DO UPDATE SET data=excluded.data, updated_at=excluded.updated_at`,
		section, string(raw), time.Now().Format(time.RFC3339),
	)
	return err
}

// ListConfig 返回全部配置分区（按键排序保证输出稳定）。
func (d *DB) ListConfig() map[string]map[string]any {
	rows, err := d.SQL.Query(`SELECT section, data FROM kv_config`)
	if err != nil {
		return map[string]map[string]any{}
	}
	defer rows.Close()
	out := map[string]map[string]any{}
	for rows.Next() {
		var section, raw string
		if err := rows.Scan(&section, &raw); err != nil {
			continue
		}
		cfg := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			continue
		}
		out[section] = cfg
	}
	// 按键排序（map 无序，排序便于调试输出）
	sections := make([]string, 0, len(out))
	for k := range out {
		sections = append(sections, k)
	}
	sort.Strings(sections)
	ordered := make(map[string]map[string]any, len(out))
	for _, k := range sections {
		ordered[k] = out[k]
	}
	return ordered
}

// ---------------- transfer_history ----------------

// InsertTransfer 插入转存历史。
func (d *DB) InsertTransfer(r *TransferRow) (int64, error) {
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().Format(time.RFC3339)
	}
	res, err := d.SQL.Exec(
		`INSERT INTO transfer_history(drive, channel, share_link, code, target_dir, status, message, result, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.Drive, r.Channel, r.ShareLink, r.Code, r.TargetDir, r.Status, r.Message, r.Result, r.CreatedAt,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// TransferHistory 分页查询转存历史，支持状态与关键字过滤。
func (d *DB) TransferHistory(drive, status, keyword string, page, size int) ([]TransferRow, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if drive != "" && !strings.EqualFold(drive, "all") {
		where = append(where, "drive = ?")
		args = append(args, drive)
	}
	if status != "" && !strings.EqualFold(status, "all") {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if keyword != "" {
		where = append(where, "(share_link LIKE ? OR message LIKE ? OR channel LIKE ?)")
		like := "%" + keyword + "%"
		args = append(args, like, like, like)
	}
	cond := strings.Join(where, " AND ")

	var total int
	_ = d.SQL.QueryRow(`SELECT COUNT(1) FROM transfer_history WHERE `+cond, args...).Scan(&total)

	query := `SELECT id, drive, channel, share_link, code, target_dir, status, message, result, created_at
	          FROM transfer_history WHERE ` + cond + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, size, (page-1)*size)

	rows, err := d.SQL.Query(query, args...)
	if err != nil {
		return nil, total
	}
	defer rows.Close()

	out := []TransferRow{}
	for rows.Next() {
		var r TransferRow
		if err := rows.Scan(&r.ID, &r.Drive, &r.Channel, &r.ShareLink, &r.Code, &r.TargetDir, &r.Status, &r.Message, &r.Result, &r.CreatedAt); err != nil {
			continue
		}
		r.DriveName = DriveNames[r.Drive]
		if r.DriveName == "" {
			r.DriveName = r.Drive
		}
		r.Category = r.Drive
		out = append(out, r)
	}
	return out, total
}

// FindTransferByLink 查询同一链接+目标目录的成功记录（用于去重）。
func (d *DB) FindTransferByLink(link, target string) (*TransferRow, bool) {
	row := d.SQL.QueryRow(
		`SELECT id, drive, channel, share_link, code, target_dir, status, message, result, created_at
		 FROM transfer_history WHERE share_link = ? AND target_dir = ? AND status = '成功' ORDER BY id DESC LIMIT 1`,
		link, target,
	)
	var r TransferRow
	if err := row.Scan(&r.ID, &r.Drive, &r.Channel, &r.ShareLink, &r.Code, &r.TargetDir, &r.Status, &r.Message, &r.Result, &r.CreatedAt); err != nil {
		return nil, false
	}
	return &r, true
}

// ---------------- organize_history ----------------

// InsertOrganize 插入整理历史。
func (d *DB) InsertOrganize(r *OrganizeRow) (int64, error) {
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().Format(time.RFC3339)
	}
	res, err := d.SQL.Exec(
		`INSERT INTO organize_history(drive, kind, original_name, target_name, target_path, tmdb_id, mbid, status, error, created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		r.Drive, r.Kind, r.OriginalName, r.TargetName, r.TargetPath, r.TMDBID, r.MBID, r.Status, r.Error, r.CreatedAt,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// OrganizeHistory 分页查询整理历史。
func (d *DB) OrganizeHistory(drive, kind, status, keyword string, page, size int) ([]OrganizeRow, int) {
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	where := []string{"1=1"}
	args := []any{}
	if drive != "" && !strings.EqualFold(drive, "all") {
		where = append(where, "drive = ?")
		args = append(args, drive)
	}
	if kind != "" && !strings.EqualFold(kind, "all") {
		where = append(where, "kind = ?")
		args = append(args, kind)
	}
	if status != "" && !strings.EqualFold(status, "all") {
		where = append(where, "status = ?")
		args = append(args, status)
	}
	if keyword != "" {
		where = append(where, "(original_name LIKE ? OR target_path LIKE ? OR tmdb_id LIKE ? OR error LIKE ?)")
		like := "%" + keyword + "%"
		args = append(args, like, like, like, like)
	}
	cond := strings.Join(where, " AND ")

	var total int
	_ = d.SQL.QueryRow(`SELECT COUNT(1) FROM organize_history WHERE `+cond, args...).Scan(&total)

	query := `SELECT id, drive, kind, original_name, target_name, target_path, tmdb_id, mbid, status, error, created_at
	          FROM organize_history WHERE ` + cond + ` ORDER BY id DESC LIMIT ? OFFSET ?`
	args = append(args, size, (page-1)*size)

	rows, err := d.SQL.Query(query, args...)
	if err != nil {
		return nil, total
	}
	defer rows.Close()

	out := []OrganizeRow{}
	for rows.Next() {
		var r OrganizeRow
		if err := rows.Scan(&r.ID, &r.Drive, &r.Kind, &r.OriginalName, &r.TargetName, &r.TargetPath, &r.TMDBID, &r.MBID, &r.Status, &r.Error, &r.CreatedAt); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, total
}

// RollbackOrganize 将指定记录回退为原始文件名并更新状态。
func (d *DB) RollbackOrganize(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	args = append(args, "已回退")
	res, err := d.SQL.Exec(
		`UPDATE organize_history SET target_name = original_name, status = ? WHERE id IN (`+strings.Join(placeholders, ",")+`)`,
		args...,
	)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeleteOrganize 批量删除整理记录。
func (d *DB) DeleteOrganize(ids []int64) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	placeholders := make([]string, len(ids))
	args := make([]any, 0, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args = append(args, id)
	}
	res, err := d.SQL.Exec(`DELETE FROM organize_history WHERE id IN (`+strings.Join(placeholders, ",")+`)`, args...)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClearOrganize 清空整理历史。
func (d *DB) ClearOrganize() (int64, error) {
	res, err := d.SQL.Exec(`DELETE FROM organize_history`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ---------------- system_logs ----------------

// InsertSystemLog 落库一条关键日志。
func (d *DB) InsertSystemLog(e logger.Entry) error {
	_, err := d.SQL.Exec(
		`INSERT INTO system_logs(level, module, message, created_at) VALUES(?,?,?,?)`,
		string(e.Level), e.Module, e.Message, e.Time.Format(time.RFC3339),
	)
	return err
}

// SystemLogs 查询落库日志（按时间正序，最多 limit 条）。
func (d *DB) SystemLogs(module string, limit int) []logger.Entry {
	if limit <= 0 || limit > 2000 {
		limit = 500
	}
	query := `SELECT level, module, message, created_at FROM system_logs`
	args := []any{}
	if module != "" && !strings.EqualFold(module, "ALL") {
		query += ` WHERE module = ?`
		args = append(args, module)
	}
	query += ` ORDER BY id DESC LIMIT ?`
	args = append(args, limit)

	rows, err := d.SQL.Query(query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()

	out := []logger.Entry{}
	for rows.Next() {
		var (
			level, module, message, createdAt string
			e                                 logger.Entry
		)
		if err := rows.Scan(&level, &module, &message, &createdAt); err != nil {
			continue
		}
		e.Level = logger.Level(level)
		e.Module = module
		e.Message = message
		if t, err := time.Parse(time.RFC3339, createdAt); err == nil {
			e.Time = t
		}
		out = append([]logger.Entry{e}, out...) // 反转为正序
	}
	return out
}
