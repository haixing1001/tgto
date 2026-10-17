package service

import (
	"context"
	"crypto/aes"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"tgto/internal/logger"
)

// 天翼云盘文件管理：列表 / 新建 / 重命名 / 移动 / 复制 / 删除 / 清空回收站 / 下载链接。
// 端点与签名机制移植自 OpenList 189pc 驱动（https://github.com/OpenListTeam/OpenList）：
//   - 业务 API 全部在 api.cloud.189.cn
//   - 鉴权三件套：SessionKey header + X-Request-ID(UUID) + Signature(HMAC-SHA1，大写 hex)
//   - POST 带参数时用 AES-ECB(sessionSecret[:16]) 加密后放 query params；
//     表单类接口（createBatchTask/createFolder/rename）参数明传，签名 params 段为空
//   - 写操作（移动/复制/删除/清空回收站）是异步批量任务：createBatchTask 拿 taskId，
//     再轮询 checkBatchTask 直到 taskStatus=4（done）

// tyClientSuffix 公共 query 参数（与 OpenList PC 客户端保持一致）。
func tyClientSuffix() url.Values {
	q := url.Values{}
	q.Set("clientType", "TELEPC")
	q.Set("version", "7.2.4.0")
	q.Set("channelId", "web_cloud.189.cn")
	q.Set("rand", fmt.Sprintf("%d_%d", time.Now().UnixNano()%100000, time.Now().UnixNano()))
	return q
}

// tyGmtDate 生成 HTTP Date 头（RFC1123 GMT 格式）。
func tyGmtDate() string {
	return time.Now().UTC().Format(http.TimeFormat)
}

// tyAesECBEncrypt AES-ECB + PKCS7 加密，输出大写 hex（key 取 sessionSecret 前 16 字节）。
// Go 标准库不提供 ECB 模式，这里按块手动加密。
func tyAesECBEncrypt(plaintext, key string) (string, error) {
	block, err := aes.NewCipher([]byte(key[:16]))
	if err != nil {
		return "", err
	}
	// PKCS7 填充
	padLen := aes.BlockSize - len(plaintext)%aes.BlockSize
	padded := make([]byte, len(plaintext)+padLen)
	copy(padded, plaintext)
	for i := len(plaintext); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}
	// 逐块 ECB 加密（无 IV）
	dst := make([]byte, len(padded))
	for i := 0; i < len(padded); i += aes.BlockSize {
		block.Encrypt(dst[i:i+aes.BlockSize], padded[i:i+aes.BlockSize])
	}
	return strings.ToUpper(hex.EncodeToString(dst)), nil
}

// tySignature 计算 HMAC-SHA1 签名（大写 hex）。
// 数据格式：SessionKey=<sk>&Operate=<method>&RequestURI=<path>&Date=<gmt>[&params=<encrypted>]
func tySignature(sessionSecret, sessionKey, operate, fullURL, dateOfGmt, params string) string {
	// 提取 URL 的 path 部分（去掉协议+域名）
	u, err := url.Parse(fullURL)
	urlpath := u.Path
	if err != nil || urlpath == "" {
		urlpath = fullURL
	}
	data := fmt.Sprintf("SessionKey=%s&Operate=%s&RequestURI=%s&Date=%s", sessionKey, operate, urlpath, dateOfGmt)
	if params != "" {
		data += fmt.Sprintf("&params=%s", params)
	}
	mac := hmac.New(sha1.New, []byte(sessionSecret))
	mac.Write([]byte(data))
	return strings.ToUpper(hex.EncodeToString(mac.Sum(nil)))
}

// tySignatureHeaders 构造业务请求头（SessionKey/X-Request-ID/Signature/Date）。
func tySignatureHeaders(sess *tySession, method, fullURL, params string) map[string]string {
	date := tyGmtDate()
	return map[string]string{
		"Accept":        "application/json;charset=UTF-8",
		"Referer":       tyWebURL,
		"Date":          date,
		"SessionKey":    sess.SessionKey,
		"X-Request-ID":  tyUUID(),
		"Signature":     tySignature(sess.SessionSecret, sess.SessionKey, method, fullURL, date, params),
	}
}

// tyBusinessGet GET 业务接口（query 参数直接放 URL + clientSuffix + 签名）。
// 注意：天翼业务 API 的 GET 请求参数不加密，直接放 query string；
// 签名数据格式为 SessionKey=<sk>&Operate=GET&RequestURI=<path>&Date=<gmt>（无 params 段）。
func (s *Services) tyBusinessGet(ctx context.Context, sess *tySession, path string, params map[string]string) (map[string]any, error) {
	fullURL := tyAPIBase + path
	query := tyClientSuffix()
	if len(params) > 0 {
		raw := url.Values{}
		for k, v := range params {
			raw.Set(k, v)
		}
		for k := range raw {
			query.Set(k, raw.Get(k))
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fullURL+"?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	for k, v := range tySignatureHeaders(sess, "GET", fullURL, "") {
		req.Header.Set(k, v)
	}
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body := tyReadBody(resp)
	decoded := tyDecodeBiz(body)
	if decoded == nil {
		s.Log.Warn(logger.ModTianYi, "天翼 GET %s 响应无法解析（%d 字节）: %s", path, len(body), truncate(body, 300))
	}
	return decoded, nil
}

// tyBusinessPostForm POST 表单接口（参数明传，签名 params 段为空）。
func (s *Services) tyBusinessPostForm(ctx context.Context, sess *tySession, path string, form url.Values) (map[string]any, error) {
	fullURL := tyAPIBase + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, stringsReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json;charset=UTF-8")
	for k, v := range tySignatureHeaders(sess, "POST", fullURL, "") {
		req.Header.Set(k, v)
	}
	resp, err := s.tyBusinessClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body := tyReadBody(resp)
	return tyDecodeBiz(body), nil
}

// tyBusinessClient 业务请求 HTTP 客户端（复用共享 Transport，20s 超时）。
func (s *Services) tyBusinessClient() *http.Client {
	return &http.Client{
		Timeout:   20 * time.Second,
		Transport: sharedHTTPClient.Transport,
	}
}

// tyDecodeBiz 解析业务响应 JSON（使用 UseNumber 避免大整数 ID 精度丢失）。
func tyDecodeBiz(body string) map[string]any {
	dec := json.NewDecoder(strings.NewReader(body))
	dec.UseNumber()
	var out map[string]any
	if err := dec.Decode(&out); err != nil {
		return nil
	}
	return out
}

// tyBizErr 提取业务错误信息。
func tyBizErr(decoded map[string]any) string {
	if decoded == nil {
		return "响应无法解析"
	}
	if msg, ok := decoded["errorMsg"].(string); ok && msg != "" {
		return msg
	}
	if msg, ok := decoded["res_message"].(string); ok && msg != "" {
		return msg
	}
	if msg, ok := decoded["msg"].(string); ok && msg != "" {
		return msg
	}
	if msg, ok := decoded["message"].(string); ok && msg != "" {
		return msg
	}
	return "接口返回失败"
}

// tyBizOk 判断业务响应是否成功。
// 天翼业务 API 成功时返回 res_code=0（数字），失败时返回 errorCode/errorMsg。
func tyBizOk(decoded map[string]any) bool {
	if decoded == nil {
		return false
	}
	// 成功格式：res_code=0
	if v, ok := decoded["res_code"]; ok {
		return tyFloat(v) == 0
	}
	// 失败格式：errorCode/errorMsg 存在即为失败
	if _, hasErr := decoded["errorCode"]; hasErr {
		return false
	}
	if _, hasErr := decoded["errorMsg"]; hasErr {
		return false
	}
	// 无 res_code 但有数据字段，视为成功
	if _, hasData := decoded["fileListAO"]; hasData {
		return true
	}
	if _, hasData := decoded["data"]; hasData {
		return true
	}
	// 默认不视为成功
	return false
}

// tyNeedLogin 判断是否需要重新扫码登录（SessionKey 失效类错误）。
func tyNeedLogin(decoded map[string]any) bool {
	if decoded == nil {
		return false
	}
	msg, _ := decoded["errorMsg"].(string)
	msg += " "
	if r, ok := decoded["res_message"].(string); ok {
		msg += r
	}
	for _, kw := range []string{"userSessionBO is null", "InvalidSessionKey", "InvalidSession", "sessionsignature is not match", "未登录", "登录已过期"} {
		if strings.Contains(msg, kw) {
			return true
		}
	}
	return false
}

// tySessionOrErr 加载会话，未登录时返回友好错误。
func (s *Services) tySessionOrErr() (*tySession, error) {
	sess, err := s.loadTySession()
	if err != nil || sess == nil || sess.SessionKey == "" {
		return nil, fmt.Errorf("未检测到天翼云盘登录信息，请先完成扫码登录")
	}
	return sess, nil
}

// ---------------- 文件管理业务函数 ----------------

// tyFileItem 统一的文件/文件夹项（合并 folderList + fileList）。
type tyFileItem struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Size       int64  `json:"size"`
	IsDir      bool   `json:"is_dir"`
	LastOpTime string `json:"last_op_time"`
	CreateDate string `json:"create_date"`
}

// TyBatchItem 批量任务项（移动/复制/删除）。
type TyBatchItem struct {
	FileID   string `json:"fileId"`
	FileName string `json:"fileName"`
	IsFolder int    `json:"isFolder"` // 0=文件 1=文件夹
}

// TyFileList 获取文件列表。
// folderID 为目录 ID，个人云根目录为 "-11"；返回 { folders: [...], files: [...], count: N }。
func (s *Services) TyFileList(ctx context.Context, folderID string, pageNum, pageSize int) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveTianYi); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	sess, err := s.tySessionOrErr()
	if err != nil {
		return nil, err
	}
	if folderID == "" {
		folderID = "-11"
	}
	params := map[string]string{
		"folderId":   folderID,
		"fileType":   "0",
		"mediaAttr":  "0",
		"iconOption": "5",
		"pageNum":    fmt.Sprintf("%d", pageNum),
		"pageSize":   fmt.Sprintf("%d", pageSize),
		"recursive":  "0",
		"orderBy":    "lastOpTime",
		"descending": "true",
	}
	decoded, err := s.tyBusinessGet(ctx, sess, "/listFiles.action", params)
	if err != nil {
		return nil, err
	}
	if !tyBizOk(decoded) {
		if tyNeedLogin(decoded) {
			return nil, fmt.Errorf("天翼云盘登录已过期，请重新扫码登录")
		}
		return nil, fmt.Errorf("获取文件列表失败: %s", tyBizErr(decoded))
	}
	// 合并 folderList + fileList 为统一列表
	var items []tyFileItem
	if fl, ok := decoded["fileListAO"].(map[string]any); ok {
		if folders, ok := fl["folderList"].([]any); ok {
			for _, f := range folders {
				if m, ok := f.(map[string]any); ok {
					items = append(items, tyFileItem{
						ID:         tyStr(m["id"]),
						Name:       tyStr(m["name"]),
						IsDir:      true,
						LastOpTime: tyStr(m["lastOpTime"]),
						CreateDate: tyStr(m["createDate"]),
					})
				}
			}
		}
		if files, ok := fl["fileList"].([]any); ok {
			for _, f := range files {
				if m, ok := f.(map[string]any); ok {
					items = append(items, tyFileItem{
						ID:         tyStr(m["id"]),
						Name:       tyStr(m["name"]),
						Size:       int64(tyFloat(m["size"])),
						IsDir:      false,
						LastOpTime: tyStr(m["lastOpTime"]),
						CreateDate: tyStr(m["createDate"]),
					})
				}
			}
		}
	}
	// 转为 JSON 友好的 map 返回
	result := map[string]any{"folders": []tyFileItem{}, "files": []tyFileItem{}, "count": len(items)}
	var folders, files []tyFileItem
	for _, it := range items {
		if it.IsDir {
			folders = append(folders, it)
		} else {
			files = append(files, it)
		}
	}
	result["folders"] = folders
	result["files"] = files
	result["count"] = len(items)
	s.Log.Info(logger.ModTianYi, "天翼文件列表：folderID=%s，共 %d 项", folderID, len(items))
	return result, nil
}

// TyCreateDir 新建文件夹。
func (s *Services) TyCreateDir(ctx context.Context, dirName, parentID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveTianYi); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	sess, err := s.tySessionOrErr()
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("folderName", dirName)
	form.Set("relativePath", "")
	form.Set("parentFolderId", parentID)
	decoded, err := s.tyBusinessPostForm(ctx, sess, "/createFolder.action", form)
	if err != nil {
		return nil, err
	}
	if !tyBizOk(decoded) {
		if tyNeedLogin(decoded) {
			return nil, fmt.Errorf("天翼云盘登录已过期，请重新扫码登录")
		}
		return nil, fmt.Errorf("新建文件夹失败: %s", tyBizErr(decoded))
	}
	s.Log.Info(logger.ModTianYi, "天翼新建文件夹：%s（父目录 %q）", dirName, parentID)
	return map[string]any{"status": "ok", "message": "文件夹已创建"}, nil
}

// TyRename 重命名文件/文件夹。
func (s *Services) TyRename(ctx context.Context, fileID, newName string, isFolder bool) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveTianYi); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	sess, err := s.tySessionOrErr()
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	path := "/renameFile.action"
	if isFolder {
		path = "/renameFolder.action"
		form.Set("folderId", fileID)
		form.Set("destFolderName", newName)
	} else {
		form.Set("fileId", fileID)
		form.Set("destFileName", newName)
	}
	decoded, err := s.tyBusinessPostForm(ctx, sess, path, form)
	if err != nil {
		return nil, err
	}
	if !tyBizOk(decoded) {
		if tyNeedLogin(decoded) {
			return nil, fmt.Errorf("天翼云盘登录已过期，请重新扫码登录")
		}
		return nil, fmt.Errorf("重命名失败: %s", tyBizErr(decoded))
	}
	s.Log.Info(logger.ModTianYi, "天翼重命名：%s → %s", fileID, newName)
	return map[string]any{"status": "ok", "message": "重命名成功"}, nil
}

// TyMove 移动文件/文件夹到目标目录。
func (s *Services) TyMove(ctx context.Context, items []TyBatchItem, targetFolderID string) (map[string]any, error) {
	return s.tyBatchTask(ctx, "MOVE", items, targetFolderID)
}

// TyCopy 复制文件/文件夹到目标目录。
func (s *Services) TyCopy(ctx context.Context, items []TyBatchItem, targetFolderID string) (map[string]any, error) {
	return s.tyBatchTask(ctx, "COPY", items, targetFolderID)
}

// TyDelete 删除文件（移入回收站）。
func (s *Services) TyDelete(ctx context.Context, items []TyBatchItem) (map[string]any, error) {
	return s.tyBatchTask(ctx, "DELETE", items, "")
}

// TyClearRecycle 清空回收站。
func (s *Services) TyClearRecycle(ctx context.Context) (map[string]any, error) {
	return s.tyBatchTask(ctx, "CLEAR_RECYCLE", nil, "")
}

// tyBatchTask 创建批量任务并等待完成。
func (s *Services) tyBatchTask(ctx context.Context, taskType string, items []TyBatchItem, targetFolderID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveTianYi); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	sess, err := s.tySessionOrErr()
	if err != nil {
		return nil, err
	}
	form := url.Values{}
	form.Set("type", taskType)
	if len(items) > 0 {
		taskInfos, _ := json.Marshal(items)
		form.Set("taskInfos", string(taskInfos))
	}
	if targetFolderID != "" {
		form.Set("targetFolderId", targetFolderID)
	}
	decoded, err := s.tyBusinessPostForm(ctx, sess, "/batch/createBatchTask.action", form)
	if err != nil {
		return nil, err
	}
	if !tyBizOk(decoded) {
		if tyNeedLogin(decoded) {
			return nil, fmt.Errorf("天翼云盘登录已过期，请重新扫码登录")
		}
		return nil, fmt.Errorf("批量任务创建失败: %s", tyBizErr(decoded))
	}
	taskID := tyStr(decoded["taskId"])
	if taskID == "" {
		// 无 taskId 说明任务已同步完成
		return map[string]any{"status": "ok", "message": "操作完成"}, nil
	}
	s.Log.Info(logger.ModTianYi, "天翼批量任务 %s 已创建，taskId=%s，等待完成...", taskType, taskID)
	// 轮询等待任务完成
	if err := s.tyWaitBatchTask(ctx, sess, taskType, taskID); err != nil {
		return nil, err
	}
	return map[string]any{"status": "ok", "message": "操作完成"}, nil
}

// tyWaitBatchTask 轮询等待批量任务完成（taskStatus=4）。
func (s *Services) tyWaitBatchTask(ctx context.Context, sess *tySession, taskType, taskID string) error {
	// 先等 400ms 让任务开始
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(400 * time.Millisecond):
	}
	for i := 0; i < 60; i++ { // 最多等 30s
		form := url.Values{}
		form.Set("type", taskType)
		form.Set("taskId", taskID)
		decoded, err := s.tyBusinessPostForm(ctx, sess, "/batch/checkBatchTask.action", form)
		if err != nil {
			return err
		}
		if !tyBizOk(decoded) {
			return fmt.Errorf("查询任务状态失败: %s", tyBizErr(decoded))
		}
		status := int(tyFloat(decoded["taskStatus"]))
		// taskStatus: 1=init 2=conflict 3=running 4=done
		if status == 4 {
			failed := int(tyFloat(decoded["failedCount"]))
			if failed > 0 {
				return fmt.Errorf("部分文件操作失败（%d 个失败）", failed)
			}
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
	return fmt.Errorf("批量任务超时（30s），请稍后刷新查看结果")
}

// TyDownloadURL 获取文件下载链接。
func (s *Services) TyDownloadURL(ctx context.Context, fileID string) (map[string]any, error) {
	if err := s.WaitLimit(ctx, DriveTianYi); err != nil {
		return nil, fmt.Errorf("等待限流令牌超时: %w", err)
	}
	sess, err := s.tySessionOrErr()
	if err != nil {
		return nil, err
	}
	params := map[string]string{
		"fileId": fileID,
		"dt":     "3",
		"flag":   "1",
	}
	decoded, err := s.tyBusinessGet(ctx, sess, "/getFileDownloadUrl.action", params)
	if err != nil {
		return nil, err
	}
	if !tyBizOk(decoded) {
		if tyNeedLogin(decoded) {
			return nil, fmt.Errorf("天翼云盘登录已过期，请重新扫码登录")
		}
		return nil, fmt.Errorf("获取下载链接失败: %s", tyBizErr(decoded))
	}
	downloadURL := tyStr(decoded["fileDownloadUrl"])
	if downloadURL == "" {
		return nil, fmt.Errorf("未获取到下载链接")
	}
	// 清理 &amp; 实体并升级为 https
	downloadURL = strings.ReplaceAll(downloadURL, "&amp;", "&")
	downloadURL = strings.Replace(downloadURL, "http://", "https://", 1)
	return map[string]any{"url": downloadURL}, nil
}

// ---------------- 工具函数 ----------------

// tyStr 从 interface{} 安全转 string（兼容 float64/string/int/json.Number）。
func tyStr(v any) string {
	switch n := v.(type) {
	case json.Number:
		return n.String()
	case string:
		return n
	case float64:
		// 大整数 ID 不能用 %g（会变科学计数法），用 %.0f 保留完整数字
		if n == float64(int64(n)) {
			return fmt.Sprintf("%.0f", n)
		}
		return fmt.Sprintf("%g", n)
	case int:
		return fmt.Sprintf("%d", n)
	case int64:
		return fmt.Sprintf("%d", n)
	}
	return ""
}

// tyFloat 从 interface{} 安全转 float64。
func tyFloat(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case json.Number:
		f, _ := n.Float64()
		return f
	case string:
		f := 0.0
		fmt.Sscanf(n, "%g", &f)
		return f
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}
