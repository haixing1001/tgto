package service

// 移动云盘（139）个人云文件管理：列表、新建目录、重命名、移动、复制、删除、回收站、下载地址。
//
// 端点均挂在路由发现的 personal host（形如 https://personal-kd-njs.yun.139.com/hcy）下：
//   /file/list、/file/create、/file/update、/file/batchMove、/file/batchCopy、
//   /recyclebin/batchTrash、/recyclebin/clear、/file/getDownloadUrl
// 移动/复制/删除/清空回收站返回 taskId，需用 /task/get 轮询。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"tgto/internal/logger"
)

// y139PersonalCtx 取令牌 + 个人云接入地址。
func (s *Services) y139PersonalCtx(ctx context.Context) (string, string, error) {
	auth, err := s.loadY139Auth()
	if err != nil || auth == nil || auth.Authorization == "" {
		return "", "", fmt.Errorf("未登录移动云盘，请先在账号页完成登录")
	}
	host, err := s.y139RouteHost(ctx, auth.Authorization)
	if err != nil {
		return "", "", err
	}
	return auth.Authorization, host, nil
}

// y139PersonalCall 调个人云接口并校验 success/code。
func (s *Services) y139PersonalCall(ctx context.Context, authorization, host, path string, payload any) ([]byte, error) {
	body, err := s.y139SignedPost(ctx, strings.TrimRight(host, "/")+path, payload, authorization)
	if err != nil {
		return nil, err
	}
	var meta struct {
		Success *bool  `json:"success"`
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &meta); err == nil {
		if meta.Success != nil && !*meta.Success {
			return nil, fmt.Errorf("%s", orDefault(meta.Message, "移动云盘接口返回失败"))
		}
		if meta.Code != "" && meta.Code != "0" && meta.Code != "0000" {
			return nil, fmt.Errorf("%s（code=%s）", orDefault(meta.Message, "移动云盘接口返回失败"), meta.Code)
		}
	}
	return body, nil
}

// y139TaskIDOf 从响应里取 taskId。
func y139TaskIDOf(body []byte) string {
	var out struct {
		Data struct {
			TaskID string `json:"taskId"`
			TaskId string `json:"taskID"`
		} `json:"data"`
	}
	_ = json.Unmarshal(body, &out)
	return orDefault(out.Data.TaskID, out.Data.TaskId)
}

// Yun139FileList 列出目录内容（分页）。
func (s *Services) Yun139FileList(ctx context.Context, parentID string, pageSize int) (map[string]any, error) {
	authorization, host, err := s.y139PersonalCtx(ctx)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 || pageSize > 500 {
		pageSize = 200
	}
	parentID = y139CatalogID(parentID)
	payload := map[string]any{
		"parentFileId":            parentID,
		"orderBy":                 "updated_at",
		"orderDirection":          "DESC",
		"pageInfo":                map[string]any{"pageCursor": "", "pageSize": pageSize},
		"imageThumbnailStyleList": []string{"Small", "Large"},
	}
	body, err := s.y139PersonalCall(ctx, authorization, host, "/file/list", payload)
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
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
	items := make([]map[string]any, 0, len(out.Data.Items))
	for _, it := range out.Data.Items {
		items = append(items, map[string]any{
			"id":         it.FileID,
			"name":       it.Name,
			"isDir":      it.Type == "folder",
			"size":       it.Size,
			"updatedAt":  it.UpdatedAt,
			"is_dir":     it.Type == "folder",
			"updated_at": it.UpdatedAt,
		})
	}
	return map[string]any{
		"parent_id": parentID,
		"count":     len(items),
		"items":     items,
		"next":      out.Data.NextPageCursor,
	}, nil
}

// Yun139Mkdir 新建目录，返回新目录 ID。
func (s *Services) Yun139Mkdir(ctx context.Context, name, parentID string) (map[string]any, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("文件夹名称不能为空")
	}
	authorization, host, err := s.y139PersonalCtx(ctx)
	if err != nil {
		return nil, err
	}
	fid, err := s.y139CreateFolder(ctx, authorization, host, y139CatalogID(parentID), name)
	if err != nil {
		return nil, err
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 新建目录 %s（fid=%s）", name, fid)
	return map[string]any{"id": fid, "name": name}, nil
}

// Yun139Rename 重命名。
func (s *Services) Yun139Rename(ctx context.Context, fileID, newName string) (map[string]any, error) {
	fileID = strings.TrimSpace(fileID)
	newName = strings.TrimSpace(newName)
	if fileID == "" || newName == "" {
		return nil, fmt.Errorf("文件 ID 与新名称不能为空")
	}
	authorization, host, err := s.y139PersonalCtx(ctx)
	if err != nil {
		return nil, err
	}
	body, err := s.y139PersonalCall(ctx, authorization, host, "/file/update",
		map[string]any{"fileId": fileID, "name": newName})
	if err != nil {
		return nil, err
	}
	if task := y139TaskIDOf(body); task != "" {
		if err := s.y139WaitTask(ctx, authorization, host, task); err != nil {
			return nil, err
		}
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 重命名 %s -> %s", fileID, newName)
	return map[string]any{"id": fileID, "name": newName}, nil
}

// Yun139Move 批量移动到目标目录。
func (s *Services) Yun139Move(ctx context.Context, fileIDs []string, targetID string) (map[string]any, error) {
	return s.y139BatchOp(ctx, "移动", "/file/batchMove", fileIDs, targetID)
}

// Yun139Copy 批量复制到目标目录。
func (s *Services) Yun139Copy(ctx context.Context, fileIDs []string, targetID string) (map[string]any, error) {
	return s.y139BatchOp(ctx, "复制", "/file/batchCopy", fileIDs, targetID)
}

func (s *Services) y139BatchOp(ctx context.Context, action, path string, fileIDs []string, targetID string) (map[string]any, error) {
	ids := y139CleanIDs(fileIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("请选择要%s的文件", action)
	}
	target := y139CatalogID(targetID)
	authorization, host, err := s.y139PersonalCtx(ctx)
	if err != nil {
		return nil, err
	}
	body, err := s.y139PersonalCall(ctx, authorization, host, path,
		map[string]any{"fileIds": ids, "toParentFileId": target})
	if err != nil {
		return nil, err
	}
	if task := y139TaskIDOf(body); task != "" {
		if err := s.y139WaitTask(ctx, authorization, host, task); err != nil {
			return nil, err
		}
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] %s %d 项 -> %s", action, len(ids), target)
	return map[string]any{"count": len(ids), "target_id": target}, nil
}

// Yun139Delete 批量删除（进回收站）。
func (s *Services) Yun139Delete(ctx context.Context, fileIDs []string) (map[string]any, error) {
	ids := y139CleanIDs(fileIDs)
	if len(ids) == 0 {
		return nil, fmt.Errorf("请选择要删除的文件")
	}
	authorization, host, err := s.y139PersonalCtx(ctx)
	if err != nil {
		return nil, err
	}
	body, err := s.y139PersonalCall(ctx, authorization, host, "/recyclebin/batchTrash",
		map[string]any{"fileIds": ids})
	if err != nil {
		return nil, err
	}
	if task := y139TaskIDOf(body); task != "" {
		if err := s.y139WaitTask(ctx, authorization, host, task); err != nil {
			return nil, err
		}
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 删除 %d 项到回收站", len(ids))
	return map[string]any{"count": len(ids)}, nil
}

// Yun139ClearRecycle 清空回收站。
func (s *Services) Yun139ClearRecycle(ctx context.Context) (map[string]any, error) {
	authorization, host, err := s.y139PersonalCtx(ctx)
	if err != nil {
		return nil, err
	}
	body, err := s.y139PersonalCall(ctx, authorization, host, "/recyclebin/clear", map[string]any{})
	if err != nil {
		return nil, err
	}
	if task := y139TaskIDOf(body); task != "" {
		_ = s.y139WaitTask(ctx, authorization, host, task)
	}
	s.Log.Info(logger.ModCloud139, "[移动云盘] 已清空回收站")
	return map[string]any{"cleared": true}, nil
}

// Yun139DownloadURL 取文件下载直链。
func (s *Services) Yun139DownloadURL(ctx context.Context, fileID string) (map[string]any, error) {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return nil, fmt.Errorf("文件 ID 不能为空")
	}
	authorization, host, err := s.y139PersonalCtx(ctx)
	if err != nil {
		return nil, err
	}
	body, err := s.y139PersonalCall(ctx, authorization, host, "/file/getDownloadUrl",
		map[string]any{"fileId": fileID})
	if err != nil {
		return nil, err
	}
	var out struct {
		Data struct {
			URL       string `json:"url"`
			CDNURL    string `json:"cdnUrl"`
			CDNSwitch bool   `json:"cdnSwitch"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("下载地址解析失败: %s", truncate(string(body), 200))
	}
	url := out.Data.URL
	if out.Data.CDNSwitch && out.Data.CDNURL != "" {
		url = out.Data.CDNURL
	}
	if url == "" {
		return nil, fmt.Errorf("未获取到下载地址: %s", truncate(string(body), 200))
	}
	return map[string]any{"url": url}, nil
}

func y139CleanIDs(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if v := strings.TrimSpace(id); v != "" {
			out = append(out, v)
		}
	}
	return out
}

// Yun139BatchItem 批量操作条目（与天翼保持同一请求形状）。
type Yun139BatchItem struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
	IsFolder int    `json:"isFolder"`
}
