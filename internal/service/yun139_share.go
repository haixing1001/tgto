package service

// 移动云盘（139）分享转存。
//
// 协议参考 cloud-save-max 的 cloud139_adapter.py（wuanqicll-del/cloud-save-max）：
//   1) share-kd-njs: /yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6 查分享目录
//   2) share-kd-njs: /yun-share/richlifeApp/devapp/IBatchOprTask/createOuterLinkBatchOprTask 建转存任务
//   3) personal-kd-njs: /hcy/task/get 轮询任务结果
// 分享里的条目在转存请求里用「分享内路径」而不是文件 ID。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tgto/internal/logger"
)

const (
	// 分享目录单页上限，与服务端默认一致
	y139SharePageSize = 200
)

// y139ShareBase 分享接口域名（测试可覆盖）。
var y139ShareBase = "https://share-kd-njs.yun.139.com"

// y139ShareNode 分享目录里的一个条目。
type y139ShareNode struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	IsDir bool   `json:"is_dir"`
	Size  int64  `json:"size"`
	Path  string `json:"path"` // 分享内路径，转存时使用
}

// Yun139ShareInfo 查询分享链接的根目录（用于界面预览与转存前校验）。
func (s *Services) Yun139ShareInfo(ctx context.Context, linkID, passwd string) (map[string]any, error) {
	linkID = strings.TrimSpace(linkID)
	if linkID == "" {
		return nil, fmt.Errorf("分享 ID 为空")
	}
	nodes, err := s.y139ShareList(ctx, linkID, passwd, "root")
	if err != nil {
		return nil, err
	}
	return map[string]any{"share_id": linkID, "count": len(nodes), "files": nodes}, nil
}

// y139ShareList 拉取分享目录（pdir 为空/root 表示根目录）。
func (s *Services) y139ShareList(ctx context.Context, linkID, passwd, pdir string) ([]y139ShareNode, error) {
	if pdir == "" {
		pdir = "root"
	}
	payload := map[string]any{
		"getOutLinkInfoReq": map[string]any{
			"account": y139PhoneOf(s.GetConfig(Drive139)),
			"linkID":  linkID,
			"passwd":  passwd,
			"pCaID":   pdir,
			"caSrt":   0,
			"coSrt":   0,
			"srtDr":   1,
			"bNum":    1,
			"eNum":    y139SharePageSize,
		},
	}
	body, err := s.y139SharePost(ctx, "/yun-share/richlifeApp/devapp/IOutLink/getOutLinkInfoV6", payload)
	if err != nil {
		return nil, err
	}
	var out struct {
		Code       any    `json:"code"`
		ResultCode any    `json:"resultCode"`
		Success    *bool  `json:"success"`
		Message    string `json:"message"`
		Desc       string `json:"desc"`
		Data       struct {
			CaLst []map[string]any `json:"caLst"`
			CoLst []map[string]any `json:"coLst"`
		} `json:"data"`
		CaLst []map[string]any `json:"caLst"`
		CoLst []map[string]any `json:"coLst"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("分享目录解析失败: %s", truncate(string(body), 200))
	}
	code := orDefault(y139AnyCode(out.Code), y139AnyCode(out.ResultCode))
	desc := orDefault(out.Desc, out.Message)
	if out.Success != nil && !*out.Success {
		return nil, fmt.Errorf("分享目录查询失败: %s", orDefault(desc, code))
	}
	// 服务端 success=true 也可能是业务错误（如外链不存在），必须看 code
	if code != "" && code != "0" && code != "0000" {
		return nil, fmt.Errorf("分享查询失败（%s）: %s", code, y139ShareCodeHint(code, desc))
	}
	caLst, coLst := out.Data.CaLst, out.Data.CoLst
	if len(caLst) == 0 && len(coLst) == 0 {
		caLst, coLst = out.CaLst, out.CoLst
	}
	nodes := make([]y139ShareNode, 0, len(caLst)+len(coLst))
	for _, f := range caLst {
		name := y139FirstString(f, "catalogName", "caName", "name")
		if name == "" {
			continue
		}
		nodes = append(nodes, y139ShareNode{
			ID:    y139FirstString(f, "catalogID", "caID", "id"),
			Name:  name,
			IsDir: true,
			Path:  y139FirstString(f, "path", "caPath"),
		})
	}
	for _, f := range coLst {
		name := y139FirstString(f, "contentName", "coName", "name")
		if name == "" {
			continue
		}
		nodes = append(nodes, y139ShareNode{
			ID:   y139FirstString(f, "contentID", "coID", "id"),
			Name: name,
			Size: y139FirstInt64(f, "coSize", "contentSize", "size"),
			Path: y139FirstString(f, "path", "coPath"),
		})
	}
	if len(nodes) == 0 {
		return nil, fmt.Errorf("分享目录为空或提取码错误: %s", orDefault(desc, "未返回任何条目"))
	}
	return nodes, nil
}

// Yun139Transfer 把分享根目录下的全部条目转存到目标目录（路径形式，如 /媒体库/电影）。
func (s *Services) Yun139Transfer(ctx context.Context, linkID, passwd, targetDir string) (string, error) {
	auth, err := s.loadY139Auth()
	if err != nil || auth == nil || auth.Authorization == "" {
		return "", fmt.Errorf("未登录移动云盘，请先在账号页完成登录")
	}
	linkID = strings.TrimSpace(linkID)
	if linkID == "" {
		return "", fmt.Errorf("分享 ID 为空")
	}
	nodes, err := s.y139ShareList(ctx, linkID, passwd, "root")
	if err != nil {
		return "", err
	}
	if len(nodes) == 0 {
		return "", fmt.Errorf("分享目录为空，无可转存内容")
	}

	host, err := s.y139RouteHost(ctx, auth.Authorization)
	if err != nil {
		return "", err
	}
	targetID, err := s.y139EnsureCatalog(ctx, auth.Authorization, host, targetDir)
	if err != nil {
		return "", err
	}

	var coPaths, caPaths []string
	for _, n := range nodes {
		p := strings.TrimSpace(n.Path)
		if p == "" {
			// 服务端没给 path 时退回用名称（根目录下的条目）
			p = "/" + n.Name
		}
		if n.IsDir {
			caPaths = append(caPaths, p)
		} else {
			coPaths = append(coPaths, p)
		}
	}

	payload := map[string]any{
		"createOuterLinkBatchOprTaskReq": map[string]any{
			"msisdn":       y139PhoneOf(s.GetConfig(Drive139)),
			"ownerAccount": "",
			"taskType":     1,
			"linkID":       linkID,
			"needPassword": passwd != "",
			"taskInfo": map[string]any{
				"linkID":          linkID,
				"needPassword":    passwd != "",
				"contentInfoList": coPaths,
				"catalogInfoList": caPaths,
				"newCatalogID":    targetID,
			},
		},
	}
	body, err := s.y139SharePost(ctx, "/yun-share/richlifeApp/devapp/IBatchOprTask/createOuterLinkBatchOprTask", payload)
	if err != nil {
		return "", err
	}
	var created struct {
		Success *bool  `json:"success"`
		Message string `json:"message"`
		Data    struct {
			TaskID string `json:"taskID"`
			TaskId string `json:"taskId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &created); err != nil {
		return "", fmt.Errorf("转存任务创建响应解析失败: %s", truncate(string(body), 200))
	}
	if created.Success != nil && !*created.Success {
		return "", fmt.Errorf("转存任务创建失败: %s", orDefault(created.Message, "未知错误"))
	}
	taskID := orDefault(created.Data.TaskID, created.Data.TaskId)
	if taskID == "" {
		return "", fmt.Errorf("转存任务未返回 taskId: %s", truncate(string(body), 200))
	}

	if err := s.y139WaitTask(ctx, auth.Authorization, host, taskID); err != nil {
		return "", err
	}
	msg := fmt.Sprintf("转存成功：%d 个文件、%d 个目录 → %s", len(coPaths), len(caPaths), orDefault(targetDir, "/"))
	s.Log.Info(logger.ModCloud139, "[移动云盘] %s（分享 %s）", msg, linkID)
	return msg, nil
}

// y139EnsureCatalog 逐级查找/创建目录，返回最末级目录 ID（根目录返回 "/"）。
func (s *Services) y139EnsureCatalog(ctx context.Context, authorization, host, dirPath string) (string, error) {
	clean := strings.Trim(strings.Join(strings.Fields(strings.ReplaceAll(dirPath, "//", "/")), ""), " ")
	clean = strings.TrimSuffix(clean, "/")
	if clean == "" || clean == "/" {
		return "/", nil
	}
	parent := "/"
	for _, seg := range strings.Split(strings.TrimPrefix(clean, "/"), "/") {
		seg = strings.TrimSpace(seg)
		if seg == "" {
			continue
		}
		items, err := s.y139ListItems(ctx, authorization, host, parent)
		if err != nil {
			return "", err
		}
		found := ""
		for _, it := range items {
			if it.IsDir && it.Name == seg {
				found = it.ID
				break
			}
		}
		if found == "" {
			created, err := s.y139CreateFolder(ctx, authorization, host, parent, seg)
			if err != nil {
				return "", err
			}
			found = created
		}
		parent = found
	}
	return parent, nil
}

// y139ListItems 列目录（只取一页 200 条，够建目录用）。
func (s *Services) y139ListItems(ctx context.Context, authorization, host, parentID string) ([]y139ShareNode, error) {
	payload := map[string]any{
		"imageThumbnailStyleList": []string{"Small", "Large"},
		"orderBy":                 "updated_at",
		"orderDirection":          "DESC",
		"pageInfo": map[string]any{
			"pageCursor": "",
			"pageSize":   200,
		},
		"parentFileId": orDefault(parentID, "/"),
	}
	body, err := s.y139SignedPost(ctx, strings.TrimRight(host, "/")+"/file/list", payload, authorization)
	if err != nil {
		return nil, err
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			Items []struct {
				FileID string `json:"fileId"`
				Name   string `json:"name"`
				Type   string `json:"type"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("目录列表解析失败: %s", truncate(string(body), 200))
	}
	if !out.Success {
		return nil, fmt.Errorf("目录列表请求失败: %s", orDefault(out.Message, "未知错误"))
	}
	items := make([]y139ShareNode, 0, len(out.Data.Items))
	for _, it := range out.Data.Items {
		items = append(items, y139ShareNode{ID: it.FileID, Name: it.Name, IsDir: it.Type == "folder"})
	}
	return items, nil
}

// y139CreateFolder 在 parentID 下创建目录，返回新目录 ID。
func (s *Services) y139CreateFolder(ctx context.Context, authorization, host, parentID, name string) (string, error) {
	payload := map[string]any{
		"parentFileId": orDefault(parentID, "/"),
		"name":         name,
		"type":         "folder",
	}
	body, err := s.y139SignedPost(ctx, strings.TrimRight(host, "/")+"/file/create", payload, authorization)
	if err != nil {
		return "", err
	}
	var out struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
		Data    struct {
			FileID string `json:"fileId"`
			ID     string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("创建目录响应解析失败: %s", truncate(string(body), 200))
	}
	if !out.Success {
		return "", fmt.Errorf("创建目录 %s 失败: %s", name, orDefault(out.Message, "未知错误"))
	}
	fid := orDefault(out.Data.FileID, out.Data.ID)
	if fid == "" {
		return "", fmt.Errorf("创建目录 %s 未返回 ID", name)
	}
	return fid, nil
}

// y139WaitTask 轮询转存任务，最多等 90 秒。
// 真实响应形如 {"data":{"taskInfo":{"status":"Succeed","progress":100},
// "batchFileResults":[{"errCode":"0000"}]}}。
func (s *Services) y139WaitTask(ctx context.Context, authorization, host, taskID string) error {
	deadline := time.Now().Add(90 * time.Second)
	var lastStatus string
	for {
		payload := map[string]any{"taskId": taskID}
		body, err := s.y139SignedPost(ctx, strings.TrimRight(host, "/")+"/task/get", payload, authorization)
		if err == nil {
			var out struct {
				Success bool   `json:"success"`
				Code    string `json:"code"`
				Message string `json:"message"`
				Data    struct {
					TaskInfo struct {
						Status   string  `json:"status"`
						Progress int     `json:"progress"`
						Message  *string `json:"message"`
					} `json:"taskInfo"`
					Status           any `json:"status"`
					TaskStatus       any `json:"taskStatus"`
					BatchFileResults []struct {
						ErrCode string `json:"errCode"`
						Message string `json:"message"`
					} `json:"batchFileResults"`
				} `json:"data"`
			}
			if json.Unmarshal(body, &out) == nil {
				status := strings.ToLower(orDefault(out.Data.TaskInfo.Status,
					orDefault(y139AnyCode(out.Data.Status), y139AnyCode(out.Data.TaskStatus))))
				lastStatus = status
				switch status {
				case "succeed", "success", "2", "finished", "completed":
					if msg := y139TaskFailures(out.Data.BatchFileResults); msg != "" {
						return fmt.Errorf("转存任务部分失败: %s", msg)
					}
					return nil
				case "failed", "fail", "3", "error":
					detail := out.Message
					if out.Data.TaskInfo.Message != nil && *out.Data.TaskInfo.Message != "" {
						detail = *out.Data.TaskInfo.Message
					}
					if msg := y139TaskFailures(out.Data.BatchFileResults); msg != "" {
						detail = msg
					}
					return fmt.Errorf("转存任务失败: %s", orDefault(detail, "未知原因"))
				}
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("转存任务超时（taskId=%s，最后状态=%s），请稍后在移动云盘查看", taskID, orDefault(lastStatus, "未知"))
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

// y139TaskFailures 汇总任务里失败的条目（errCode 非 0000/0）。
func y139TaskFailures(results []struct {
	ErrCode string `json:"errCode"`
	Message string `json:"message"`
}) string {
	var fails []string
	for _, r := range results {
		if r.ErrCode != "" && r.ErrCode != "0" && r.ErrCode != "0000" {
			fails = append(fails, orDefault(r.Message, r.ErrCode))
		}
	}
	if len(fails) == 0 {
		return ""
	}
	return strings.Join(fails, "；")
}

// y139SharePost 调 share-kd-njs 接口（不需要登录态，但需要账号在请求体里）。
func (s *Services) y139SharePost(ctx context.Context, path string, payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, y139ShareBase+path, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Caller", "web")
	req.Header.Set("Mcloud-Channel", "1000101")
	req.Header.Set("Mcloud-Client", "10701")
	req.Header.Set("Mcloud-Version", "7.17.2")
	req.Header.Set("Origin", "https://yun.139.com")
	req.Header.Set("Referer", "https://yun.139.com/")
	req.Header.Set("x-m4c-caller", "PC")
	req.Header.Set("x-m4c-src", "10002")
	req.Header.Set("x-inner-ntwk", "2")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36")
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("分享接口请求失败（HTTP %d）: %s", resp.StatusCode, truncate(string(body), 200))
	}
	return body, nil
}

// y139CatalogID 规范化目录 ID：空、root、0 都表示根目录 "/"。
func y139CatalogID(id string) string {
	switch strings.TrimSpace(id) {
	case "", "/", "root", "0":
		return "/"
	}
	return strings.TrimSpace(id)
}

// y139PhoneOf 从配置里取手机号（账号字段或 Authorization 内解析）。
func y139PhoneOf(cfg map[string]any) string {
	if p := strings.TrimSpace(toString(cfg["username"])); p != "" {
		return p
	}
	return y139AuthorizationAccount(toString(cfg["authorization"]))
}

// y139ShareCodeHint 把分享接口常见错误码翻成可操作的提示。
func y139ShareCodeHint(code, desc string) string {
	switch code {
	case "200000727":
		return "分享链接不存在或已被取消，请确认链接是否有效"
	case "200000728", "200000729":
		return "提取码错误，请检查后重试"
	}
	if desc != "" {
		return desc
	}
	return "未知错误"
}

func y139FirstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			if s := strings.TrimSpace(toString(v)); s != "" {
				return s
			}
		}
	}
	return ""
}

func y139FirstInt64(m map[string]any, keys ...string) int64 {
	for _, k := range keys {
		switch v := m[k].(type) {
		case float64:
			return int64(v)
		case int64:
			return v
		case string:
			if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return n
			}
		}
	}
	return 0
}

func y139AnyCode(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatInt(int64(t), 10)
	case json.Number:
		return t.String()
	}
	return ""
}
