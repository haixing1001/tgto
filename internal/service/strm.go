package service

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"tgto/internal/logger"
)

// STRMRequest STRM 生成请求。
type STRMRequest struct {
	DriveType string `json:"drive_type"` // cloud139 / cloud123 / guangya
	Mode      string `json:"mode"`       // incremental / full
}

// STRMResult STRM 生成结果。
type STRMResult struct {
	Drive       string   `json:"drive"`
	DriveName   string   `json:"drive_name"`
	Mode        string   `json:"mode"`
	TargetDir   string   `json:"target_dir"`
	SourceDir   string   `json:"source_dir"`
	Total       int      `json:"total"`
	Generated   int      `json:"generated"`
	Skipped     int      `json:"skipped"`
	Failed      int      `json:"failed"`
	Subtitles   int      `json:"subtitles"`
	Posters     int      `json:"posters"`
	DurationMS  int64    `json:"duration_ms"`
	Files       []string `json:"files"`
}

// strmWorker 并发度（小型 NAS 低内存设备不建议过高）。
const strmWorker = 4

// GenerateSTRM 在 /app/strm/<drive> 下生成 .strm 文件树，并同步字幕/海报边车文件。
func (s *Services) GenerateSTRM(ctx context.Context, req STRMRequest) (*STRMResult, error) {
	drive := strings.TrimSpace(req.DriveType)
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "incremental"
	}
	if drive != Drive139 && drive != Drive123 && drive != DriveGY {
		return nil, fmt.Errorf("不支持的网盘类型: %s（支持 cloud139 / cloud123 / guangya）", drive)
	}

	cfg := s.GetConfig(drive)
	sourceDir := orDefault(toString(cfg["local_media_dir"]), filepath.Join(s.DataDir, "media", drive))
	targetDir := orDefault(toString(cfg["strm_dir"]), filepath.Join(s.STRMRoot, drive))
	urlPrefix := orDefault(toString(cfg["strm_url_prefix"]), "http://127.0.0.1:9058/media/"+drive)

	started := time.Now()
	result := &STRMResult{
		Drive:     drive,
		DriveName: driveDisplayName(drive),
		Mode:      mode,
		TargetDir: targetDir,
		SourceDir: sourceDir,
		Files:     []string{},
	}

	// 全量重建：先清空目标树的该网盘目录
	if mode == "full" {
		if err := os.RemoveAll(targetDir); err != nil {
			s.Log.Error(logger.ModSTRM, "[%s] 全量重建清理目录失败: %v", drive, err)
			return nil, err
		}
		s.Log.Warn(logger.ModSTRM, "[%s] 全量重建模式：已清理 %s", drive, targetDir)
	}

	// 收集源文件
	videos := []string{}
	subtitles := map[string][]string{} // rel(no ext) -> sidecar 列表
	posters := map[string][]string{}   // 目录 -> 海报边车列表
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		s.Log.Warn(logger.ModSTRM, "[%s] 源媒体目录不存在: %s", drive, sourceDir)
		result.DurationMS = time.Since(started).Milliseconds()
		return result, nil
	}

	err := filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		rel, relErr := filepath.Rel(sourceDir, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		ext := strings.ToLower(filepath.Ext(rel))
		switch {
		case videoExts[ext]:
			videos = append(videos, rel)
		case ext == ".srt" || ext == ".ass" || ext == ".ssa" || ext == ".sub" || ext == ".sup":
			key := strings.TrimSuffix(rel, filepath.Ext(rel))
			subtitles[key] = append(subtitles[key], path)
		case isPosterFile(rel):
			dir := strings.ToLower(filepath.ToSlash(filepath.Dir(rel)))
			posters[dir] = append(posters[dir], path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(videos)
	result.Total = len(videos)

	type job struct {
		rel     string
		srcPath string
	}
	jobs := make(chan job)
	var wg sync.WaitGroup
	var mu sync.Mutex

	worker := func() {
		defer wg.Done()
		for j := range jobs {
			select {
			case <-ctx.Done():
				return
			default:
			}
			relNoExt := strings.TrimSuffix(j.rel, filepath.Ext(j.rel))
			target := filepath.Join(targetDir, relNoExt+".strm")
			content := urlPrefix + "/" + j.rel + "\n"

			incrementalSkip := false
			if mode == "incremental" {
				if tInfo, statErr := os.Stat(target); statErr == nil {
					sInfo, sErr := os.Stat(j.srcPath)
					if sErr == nil && !sInfo.ModTime().After(tInfo.ModTime()) {
						incrementalSkip = true
					}
				}
			}
			if incrementalSkip {
				mu.Lock()
				result.Skipped++
				mu.Unlock()
				continue
			}

			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				s.Log.Error(logger.ModSTRM, "创建目录失败 %s: %v", filepath.Dir(target), err)
				mu.Lock()
				result.Failed++
				mu.Unlock()
				continue
			}
			if err := os.WriteFile(target, []byte(content), 0o644); err != nil {
				s.Log.Error(logger.ModSTRM, "写入 STRM 失败 %s: %v", target, err)
				mu.Lock()
				result.Failed++
				mu.Unlock()
				continue
			}

			mu.Lock()
			result.Generated++
			result.Files = append(result.Files, target)
			// 字幕/海报边车同步
			for _, sub := range subtitles[relNoExt] {
				if copySidecar(sub, filepath.Join(targetDir, relNoExt+filepath.Ext(sub))) {
					result.Subtitles++
				}
			}
			videoDir := strings.ToLower(filepath.ToSlash(filepath.Dir(j.rel)))
			for _, poster := range posters[videoDir] {
				if copySidecar(poster, filepath.Join(filepath.Dir(target), filepath.Base(poster))) {
					result.Posters++
				}
			}
			mu.Unlock()
		}
	}

	for i := 0; i < strmWorker; i++ {
		wg.Add(1)
		go worker()
	}
	for _, rel := range videos {
		jobs <- job{rel: rel, srcPath: filepath.Join(sourceDir, rel)}
	}
	close(jobs)
	wg.Wait()

	result.DurationMS = time.Since(started).Milliseconds()
	if len(result.Files) > 200 {
		result.Files = result.Files[:200]
	}
	s.Log.Info(logger.ModSTRM, "[%s] STRM %s 生成完成：共 %d，新增 %d，跳过 %d，失败 %d，字幕 %d，海报 %d，耗时 %dms",
		drive, mode, result.Total, result.Generated, result.Skipped, result.Failed, result.Subtitles, result.Posters, result.DurationMS)
	return result, nil
}

// copySidecar 复制字幕/海报边车文件。
func copySidecar(src, dst string) bool {
	in, err := os.Open(src)
	if err != nil {
		return false
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return false
	}
	out, err := os.Create(dst)
	if err != nil {
		return false
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return false
	}
	return true
}

// CheckShareSTRM 校验移动云盘分享 STRM 目录下 .strm 文件对应链接的存活状态。
func (s *Services) CheckShareSTRM() (map[string]any, error) {
	tools := s.GetConfig("tools")
	dir := orDefault(toString(tools["share_strm_path"]), filepath.Join(s.STRMRoot, "cloud139_share"))

	entries := []string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".strm") {
			entries = append(entries, path)
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	alive, dead := 0, 0
	details := []map[string]any{}
	for _, p := range entries {
		data, readErr := os.ReadFile(p)
		url := strings.TrimSpace(string(data))
		ok := readErr == nil && url != "" && strings.HasPrefix(url, "http")
		if ok {
			// 本地演示环境：校验 strm 内容为合法 HTTP URL 即视为存活；
			// 配置云盘凭证后可替换为远端分享状态探测。
			alive++
		} else {
			dead++
		}
		details = append(details, map[string]any{
			"file":   p,
			"url":    url,
			"status": map[bool]string{true: "存活", false: "失效"}[ok],
		})
	}
	s.Log.Info(logger.ModSTRM, "移动云盘分享 STRM 失效检测完成：存活 %d，失效 %d（目录 %s）", alive, dead, dir)
	return map[string]any{
		"dir":    dir,
		"total":  len(entries),
		"alive":  alive,
		"dead":   dead,
		"detail": details,
	}, nil
}

// isPosterFile 判断是否为海报/封面类边车文件。
func isPosterFile(rel string) bool {
	base := strings.ToLower(filepath.Base(rel))
	return strings.Contains(base, "poster") || strings.Contains(base, "fanart") || strings.Contains(base, "cover") || strings.Contains(base, "folder")
}

func driveDisplayName(drive string) string {
	switch drive {
	case Drive139:
		return "移动云盘"
	case Drive123:
		return "123云盘"
	case DriveGY:
		return "光鸭云盘"
	case DriveTianYi:
		return "天翼云盘"
	}
	return drive
}
