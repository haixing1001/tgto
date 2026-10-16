package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"tgto/internal/logger"
)

// 光鸭云盘文件管理：文件列表 / 详情 / 新建 / 重命名 / 移动 / 复制 / 删除 /
// 回收站还原 / 清空回收站 / 下载链接 / 任务状态。
// 端点与请求体移植自参考客户端 https://github.com/DDSRem-Dev/guangyaclient ，
// 业务请求头为 authorization + did + dt + traceparent（见 gyBusinessHeaders）。

// gyTraceparent 生成 W3C traceparent 头（00-<32hex traceId>-<16hex parentId>-01）。
func gyTraceparent() string {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("00-%032x-%016x-01", time.Now().UnixNano(), time.Now().UnixNano())
	}
	return "00-" + hex.EncodeToString(buf[:16]) + "-" + hex.EncodeToString(buf[16:]) + "-01"
}

// gyBusinessHeaders 构造业务接口请求头（api.guangyapan.com 域名）。
func (s *Services) gyBusinessHeaders(tok *GyToken) map[string]string {
	deviceID := s.gyDeviceID()
	return map[string]string{
		"accept":       "application/json, text/plain, */*",
		"content-type": "application/json",
		"authorization": "Bearer " + tok.AccessToken,
		"did":          deviceID,
		"dt":           "4",
		"origin":       "https://www.guangyapan.com",
		"referer":      "https://www.guangyapan.com/",
		"user-agent":   "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/147.0.0.0 Safari/537.36",
		"traceparent":  gyTraceparent(),
	}
}

// gyBusinessPost 带业务头发起 POST 并返回解析后的响应（token 失效时自动刷新重试一次）。
func (s *Services) gyBusinessPost(ctx context.Context, path string, payload string) (map[string]any, error) {
	tok, err := s.loadGyToken()
	if err != nil || tok == nil || tok.AccessToken == "" {
		return nil, fmt.Errorf("未检测到已保存的光鸭登录信息，请先完成短信登录")
	}
	endpoint := gyAPIBase + path
	decoded, err := s.gyBusinessPostOnce(ctx, endpoint, payload, tok)
	if err != nil {
		return nil, err
	}
	// Token 失效（code 117 / 401 类）时刷新并重试一次。
	if gyIsTokenInvalid(decoded) {
		if _, rErr := s.GyRefreshToken(ctx); rErr == nil {
			tok, err = s.loadGyToken()
			if err == nil && tok != nil {
				decoded, err = s.gyBusinessPostOnce(ctx, endpoint, payload, tok)
				if err != nil {
					return nil, err
				}
			}
		}
	}
	return decoded, nil
}

// gyBusinessPostOnce 单次业务 POST。
// 注意：部分上游接口（如 rename/recycle）操作成功但返回 code=400 + msg="success"，
// gyIsSuccess 已兼容此情况；若仍返回 error 且响应体含 msg="success"，也视为成功。
func (s *Services) gyBusinessPostOnce(ctx context.Context, endpoint, payload string, tok *GyToken) (map[string]any, error) {
	headers := s.gyBusinessHeaders(tok)
	resp, err := httpPostJSONWithHeaders(ctx, endpoint, payload, headers)
	if err != nil {
		return nil, err
	}
	decoded := gyDecodeBody(resp)
	if decoded == nil {
		return nil, fmt.Errorf("响应无法解析为 JSON（%s）", truncate(resp, 120))
	}
	if !gyIsSuccess(decoded) {
		// 上游 rename/recycle 成功时返回 code=400 + msg="success"，视为成功。
		if gyMsgIsSuccess(decoded) {
			return decoded, nil
		}
		return nil, fmt.Errorf("%s", gyErrMessage(decoded, "接口返回失败"))
	}
	return decoded, nil
}

// gyIsTokenInvalid 判断业务响应是否为 Token 失效。
func gyIsTokenInvalid(decoded map[string]any) bool {
	if v, ok := decoded["code"].(float64); ok {
		return v == 117 || v == 401
	}
	if v, ok := decoded["code"].(string); ok {
		return v == "117" || v == "401"
	}
	return false
}

// GyFileList 获取文件列表。
// parentId 为空串表示根目录，"*" 表示全部目录，dirType=4 表示回收站。
func (s *Services) GyFileList(ctx context.Context, parentID string, page, pageSize, orderBy, sortType int, fileTypes []int, resType int, dirType int, needPlayRecord bool) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	data := map[string]any{
		"parentId":  parentID,
		"page":      page,
		"pageSize":  pageSize,
		"orderBy":   orderBy,
		"sortType":  sortType,
	}
	if len(fileTypes) > 0 {
		data["fileTypes"] = fileTypes
	}
	if resType > 0 {
		data["resType"] = resType
	}
	if dirType > 0 {
		data["dirType"] = dirType
	}
	if needPlayRecord {
		data["needPlayRecord"] = true
	}
	raw, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	return s.gyBusinessPost(ctx, "/userres/v1/file/get_file_list", string(raw))
}

// GyFileDetail 获取文件详情。
func (s *Services) GyFileDetail(ctx context.Context, fileID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/get_file_detail",
		fmt.Sprintf(`{"fileId":%q}`, fileID))
}

// GyCreateDir 新建文件夹。
func (s *Services) GyCreateDir(ctx context.Context, dirName, parentID string, failIfNameExist bool) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	body := fmt.Sprintf(`{"dirName":%q,"parentId":%q`, dirName, parentID)
	if failIfNameExist {
		body += `,"failIfNameExist":true`
	}
	body += "}"
	s.Log.Info(logger.ModGuangYa, "光鸭新建文件夹：%s（父目录 %q）", dirName, parentID)
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/create_dir", body)
}

// GyRename 重命名文件/文件夹。
func (s *Services) GyRename(ctx context.Context, fileID, newName string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭重命名：%s → %s", fileID, newName)
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/rename",
		fmt.Sprintf(`{"fileId":%q,"newName":%q}`, fileID, newName))
}

// GyMove 移动文件/文件夹到目标目录。
func (s *Services) GyMove(ctx context.Context, fileIDs []string, parentID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭移动 %d 个文件到 %q", len(fileIDs), parentID)
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/move_file",
		fmt.Sprintf(`{"fileIds":%s,"parentId":%q}`, gyStringSliceJSON(fileIDs), parentID))
}

// GyCopy 复制文件/文件夹到目标目录。
func (s *Services) GyCopy(ctx context.Context, fileIDs []string, parentID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭复制 %d 个文件到 %q", len(fileIDs), parentID)
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/copy_file",
		fmt.Sprintf(`{"fileIds":%s,"parentId":%q}`, gyStringSliceJSON(fileIDs), parentID))
}

// GyDelete 删除文件（普通文件移入回收站；回收站文件永久删除）。
func (s *Services) GyDelete(ctx context.Context, fileIDs []string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭删除 %d 个文件", len(fileIDs))
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/delete_file",
		fmt.Sprintf(`{"fileIds":%s}`, gyStringSliceJSON(fileIDs)))
}

// GyRecycle 回收站文件还原。
func (s *Services) GyRecycle(ctx context.Context, fileIDs []string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭还原 %d 个回收站文件", len(fileIDs))
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/recycle_file",
		fmt.Sprintf(`{"fileIds":%s}`, gyStringSliceJSON(fileIDs)))
}

// GyClearRecycleBin 清空回收站。
func (s *Services) GyClearRecycleBin(ctx context.Context) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	s.Log.Info(logger.ModGuangYa, "光鸭清空回收站")
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/file/clear_recycle_bin", "{}")
}

// GyDownloadURL 获取文件下载链接。
func (s *Services) GyDownloadURL(ctx context.Context, fileID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/get_res_download_url",
		fmt.Sprintf(`{"fileId":%q}`, fileID))
}

// GyTaskStatus 查询异步任务状态（移动/复制/还原/清空回收站）。
func (s *Services) GyTaskStatus(ctx context.Context, taskID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveGY); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	return s.gyBusinessPost(ctx, "/nd.bizuserres.s/v1/get_task_status",
		fmt.Sprintf(`{"taskId":%q}`, taskID))
}

// gyStringSliceJSON 将字符串切片编码为 JSON 数组文本。
func gyStringSliceJSON(items []string) string {
	quoted := make([]string, len(items))
	for i, it := range items {
		quoted[i] = strconv.Quote(it)
	}
	return "[" + strings.Join(quoted, ",") + "]"
}
