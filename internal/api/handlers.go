package api

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"tgto/internal/db"
	"tgto/internal/logger"
	"tgto/internal/service"
)

// ---------------- 认证 ----------------

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (h *Handlers) handleLogin(c *gin.Context) {
	var req loginRequest
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.Username) != h.adminUser || req.Password != h.adminPass {
		h.log.Warn(logger.ModAPI, "登录失败：用户名或密码错误（%s）", req.Username)
		fail(c, http.StatusUnauthorized, "用户名或密码错误")
		return
	}
	token := h.svc.CreateSession(req.Username)
	h.log.Info(logger.ModAPI, "用户 %s 登录成功", req.Username)
	ok(c, gin.H{
		"token":    token,
		"username": req.Username,
		"version":  h.version,
	})
}

func (h *Handlers) handleLogout(c *gin.Context) {
	token, _ := c.Get("token")
	if s, ok := token.(string); ok {
		h.svc.DestroySession(s)
	}
	ok(c, gin.H{"logged_out": true})
}

// ---------------- 全局状态 ----------------

// handleState 一次性返回前端所需的全部状态（配置 / 历史 / 日志）。
func (h *Handlers) handleState(c *gin.Context) {
	transfer, _ := h.svc.DB.TransferHistory("", "", "", 1, 200)
	organize, _ := h.svc.DB.OrganizeHistory("", "", "", "", 1, 200)
	logs := h.svc.Log.Query("", nil, 300)
	ok(c, gin.H{
		"version":         h.version,
		"config":          h.svc.AllConfigs(),
		"transfer_history": transfer,
		"organize_history": organize,
		"logs":            logs,
		"server_time":     time.Now().Format(time.RFC3339),
	})
}

// ---------------- 配置 ----------------

func (h *Handlers) handleGetConfig(c *gin.Context) {
	section := c.Param("section")
	if _, ok := h.svc.DB.GetConfig(section); !ok {
		if _, exists := db.DefaultConfigs()[section]; !exists {
			fail(c, http.StatusNotFound, "配置分区不存在: "+section)
			return
		}
	}
	ok(c, h.svc.GetConfig(section))
}

func (h *Handlers) handleSaveConfig(c *gin.Context) {
	section := c.Param("section")
	if _, exists := db.DefaultConfigs()[section]; !exists {
		fail(c, http.StatusNotFound, "配置分区不存在: "+section)
		return
	}
	var patch map[string]any
	if bindJSON(c, &patch) != nil {
		return
	}
	saved, err := h.svc.SaveConfig(section, patch)
	if err != nil {
		fail(c, http.StatusInternalServerError, "保存失败: "+err.Error())
		return
	}
	ok(c, saved)
}

// ---------------- 日志中心 ----------------

func (h *Handlers) handleLogs(c *gin.Context) {
	module := c.Query("module")
	limit := 500
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 2000 {
			limit = n
		}
	}
	ok(c, gin.H{
		"logs":  h.svc.Log.Query(module, nil, limit),
		"total": h.svc.Log.Count(),
	})
}

// handleMockLog 模拟一批新任务日志（前端“模拟新任务日志”按钮）。
func (h *Handlers) handleMockLog(c *gin.Context) {
	var req struct {
		Count int `json:"count"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Count <= 0 {
		req.Count = 5
	}
	if req.Count > 20 {
		req.Count = 20
	}
	modules := []string{logger.ModSystem, logger.ModScheduler, logger.ModCloud115, logger.ModCloud123, logger.ModOrganizer, logger.ModSTRM, logger.ModTransfer}
	messages := []string{
		"开始扫描频道新消息",
		"分享链接解析完成，进入转存队列",
		"TMDB 匹配成功，准备重命名",
		"STRM 增量生成中",
		"限流器令牌补充完成（QPS=2 Burst=4）",
		"整理任务归档完成",
		"心跳正常",
	}
	entries := make([]logger.Entry, 0, req.Count)
	for i := 0; i < req.Count; i++ {
		level := logger.LevelInfo
		if i%7 == 3 {
			level = logger.LevelWarn
		}
		if i%11 == 5 {
			level = logger.LevelError
		}
		entries = append(entries, h.svc.Log.Log(level, modules[i%len(modules)], "[模拟] %s", messages[i%len(messages)]))
	}
	ok(c, gin.H{"logs": entries})
}

func (h *Handlers) handleDownloadLog(c *gin.Context) {
	module := c.Query("module")
	content := h.svc.Log.Export(module)
	if content == "" {
		content = time.Now().Format("2006-01-02 15:04:05") + " [INFO] [System] 暂无日志记录\n"
	}
	c.Header("Content-Disposition", "attachment; filename=Tgto-runtime.log")
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(content))
}

// ---------------- 监控历史 / 转存 ----------------

func (h *Handlers) handleTransferHistory(c *gin.Context) {
	drive := c.Query("drive")
	status := c.Query("status")
	keyword := c.Query("keyword")
	page := queryInt(c, "page", 1)
	size := queryInt(c, "size", 10)
	rows, total := h.svc.DB.TransferHistory(drive, status, keyword, page, size)
	ok(c, gin.H{"items": rows, "total": total, "page": page, "size": size})
}

type transferSubmitRequest struct {
	Link      string `json:"link"`
	Code      string `json:"code"`
	TargetDir string `json:"target_dir"`
	Channel   string `json:"channel"`
}

func (h *Handlers) handleTransferSubmit(c *gin.Context) {
	var req transferSubmitRequest
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.Link) == "" {
		fail(c, http.StatusBadRequest, "分享链接不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	row, err := h.svc.SubmitTransfer(ctx, service.TransferSubmitRequest{
		Link:      req.Link,
		Code:      req.Code,
		TargetDir: req.TargetDir,
		Channel:   req.Channel,
	})
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, row)
}

// ---------------- 整理历史 ----------------

func (h *Handlers) handleOrganizeHistory(c *gin.Context) {
	drive := c.Query("drive")
	kind := c.Query("kind")
	status := c.Query("status")
	keyword := c.Query("keyword")
	page := queryInt(c, "page", 1)
	size := queryInt(c, "size", 10)
	rows, total := h.svc.DB.OrganizeHistory(drive, kind, status, keyword, page, size)
	ok(c, gin.H{"items": rows, "total": total, "page": page, "size": size})
}

func (h *Handlers) handleOrganizeTest(c *gin.Context) {
	var req struct {
		Filename string `json:"filename"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.Filename) == "" {
		fail(c, http.StatusBadRequest, "文件名不能为空")
		return
	}
	pm, cfg, err := h.svc.TestParse(req.Filename)
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, gin.H{"parsed": pm, "config": cfg})
}

func (h *Handlers) handleOrganizeRun(c *gin.Context) {
	var req struct {
		Drive string `json:"drive"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	drive := req.Drive
	if _, ok := db.DriveNames[drive]; !ok {
		fail(c, http.StatusBadRequest, "不支持的网盘: "+drive)
		return
	}
	success, failed, err := h.svc.OrganizeRun(drive)
	if err != nil {
		fail(c, http.StatusInternalServerError, "整理任务失败: "+err.Error())
		return
	}
	ok(c, gin.H{"success": success, "failed": failed})
}

type idsRequest struct {
	IDs []int64 `json:"ids"`
}

func (h *Handlers) handleOrganizeRollback(c *gin.Context) {
	var req idsRequest
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "未选择任何记录")
		return
	}
	n, err := h.svc.DB.RollbackOrganize(req.IDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, "回退失败: "+err.Error())
		return
	}
	h.svc.Log.Info(logger.ModOrganizer, "批量改名回退完成：%d 条记录", n)
	ok(c, gin.H{"affected": n})
}

func (h *Handlers) handleOrganizeDelete(c *gin.Context) {
	var req idsRequest
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.IDs) == 0 {
		fail(c, http.StatusBadRequest, "未选择任何记录")
		return
	}
	n, err := h.svc.DB.DeleteOrganize(req.IDs)
	if err != nil {
		fail(c, http.StatusInternalServerError, "删除失败: "+err.Error())
		return
	}
	ok(c, gin.H{"affected": n})
}

func (h *Handlers) handleOrganizeClear(c *gin.Context) {
	n, err := h.svc.DB.ClearOrganize()
	if err != nil {
		fail(c, http.StatusInternalServerError, "清空失败: "+err.Error())
		return
	}
	h.svc.Log.Warn(logger.ModOrganizer, "整理历史已清空（%d 条）", n)
	ok(c, gin.H{"affected": n})
}

// ---------------- STRM ----------------

func (h *Handlers) handleSTRMGenerate(c *gin.Context) {
	var req struct {
		DriveType string `json:"drive_type"`
		Mode      string `json:"mode"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Minute)
	defer cancel()
	result, err := h.svc.GenerateSTRM(ctx, service.STRMRequest{DriveType: req.DriveType, Mode: req.Mode})
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, result)
}

// ---------------- 实用工具 ----------------

func (h *Handlers) handleProxyTest(c *gin.Context) {
	var req struct {
		Proxy string `json:"proxy"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	res, err := h.svc.TestProxy(ctx, req.Proxy)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTelegramTest(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	if err := h.svc.SendTelegramMessage(ctx, "<b>Tgto</b> 测试消息：Telegram Bot 配置正常 ✅"); err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, gin.H{"sent": true})
}

func (h *Handlers) handleTMDBTest(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	res, err := h.svc.TestTMDBKey(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleSTRMCheck(c *gin.Context) {
	res, err := h.svc.CheckShareSTRM()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, res)
}

// ---------------- 光鸭云盘短信验证码登录 ----------------

func (h *Handlers) handleGuangyaStatus(c *gin.Context) {
	ok(c, h.svc.GyStatus())
}

func (h *Handlers) handleGuangyaSendCode(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GySendCode(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaLogin(c *gin.Context) {
	var req struct {
		Code string `json:"code"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyVerifyLogin(ctx, req.Code)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaRefresh(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyRefreshToken(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaVerify(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyVerifyToken(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaClear(c *gin.Context) {
	res, err := h.svc.GyClearLogin()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, res)
}

// ---------------- 天翼云盘扫码登录 ----------------

func (h *Handlers) handleTianyiStatus(c *gin.Context) {
	ok(c, h.svc.TyStatus())
}

func (h *Handlers) handleTianyiQRStart(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.svc.TyQRStart(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiQRStatus(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.TyQRStatus(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiQRCancel(c *gin.Context) {
	ok(c, h.svc.TyQRCancel())
}

func (h *Handlers) handleTianyiClear(c *gin.Context) {
	res, err := h.svc.TyClearLogin()
	if err != nil {
		fail(c, http.StatusInternalServerError, err.Error())
		return
	}
	ok(c, res)
}

// ---------------- 天翼云盘文件管理 ----------------

func (h *Handlers) handleTianyiFiles(c *gin.Context) {
	folderID := c.Query("folder_id")
	page := queryInt(c, "page", 1)
	pageSize := queryInt(c, "page_size", 1000)
	if page < 1 {
		page = 1
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.TyFileList(ctx, folderID, page, pageSize)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiCreateDir(c *gin.Context) {
	var req struct {
		DirName  string `json:"dir_name"`
		ParentID string `json:"parent_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.DirName) == "" {
		fail(c, http.StatusBadRequest, "dir_name 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.TyCreateDir(ctx, req.DirName, req.ParentID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiRename(c *gin.Context) {
	var req struct {
		FileID   string      `json:"file_id"`
		NewName  string      `json:"new_name"`
		IsFolder interface{} `json:"is_folder"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.FileID) == "" || strings.TrimSpace(req.NewName) == "" {
		fail(c, http.StatusBadRequest, "file_id 与 new_name 不能为空")
		return
	}
	// is_folder 兼容 bool / number(0/1) / string
	isFolder := false
	switch v := req.IsFolder.(type) {
	case bool:
		isFolder = v
	case float64:
		isFolder = v != 0
	case string:
		isFolder = v == "1" || v == "true"
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.TyRename(ctx, req.FileID, req.NewName, isFolder)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiMove(c *gin.Context) {
	var req struct {
		Items           []service.TyBatchItem `json:"items"`
		TargetFolderID  string                `json:"target_folder_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.Items) == 0 {
		fail(c, http.StatusBadRequest, "items 不能为空")
		return
	}
	if strings.TrimSpace(req.TargetFolderID) == "" {
		fail(c, http.StatusBadRequest, "target_folder_id 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	res, err := h.svc.TyMove(ctx, req.Items, req.TargetFolderID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiCopy(c *gin.Context) {
	var req struct {
		Items           []service.TyBatchItem `json:"items"`
		TargetFolderID  string                `json:"target_folder_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.Items) == 0 {
		fail(c, http.StatusBadRequest, "items 不能为空")
		return
	}
	if strings.TrimSpace(req.TargetFolderID) == "" {
		fail(c, http.StatusBadRequest, "target_folder_id 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	res, err := h.svc.TyCopy(ctx, req.Items, req.TargetFolderID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiDelete(c *gin.Context) {
	var req struct {
		Items []service.TyBatchItem `json:"items"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.Items) == 0 {
		fail(c, http.StatusBadRequest, "items 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	res, err := h.svc.TyDelete(ctx, req.Items)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiClearRecycle(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 40*time.Second)
	defer cancel()
	res, err := h.svc.TyClearRecycle(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleTianyiDownloadURL(c *gin.Context) {
	var req struct {
		FileID string `json:"file_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.FileID) == "" {
		fail(c, http.StatusBadRequest, "file_id 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.TyDownloadURL(ctx, req.FileID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

// ---------------- 光鸭云盘文件管理 ----------------

func (h *Handlers) handleGuangyaFiles(c *gin.Context) {
	parentID := c.Query("parent_id")
	page := queryInt(c, "page", 0)
	pageSize := queryInt(c, "page_size", 50)
	orderBy := queryInt(c, "order_by", 3)
	sortType := queryInt(c, "sort_type", 1)
	resType := queryInt(c, "res_type", 0)
	dirType := queryInt(c, "dir_type", 0)
	needPlayRecord := c.Query("need_play_record") == "true" || c.Query("need_play_record") == "1"
	var fileTypes []int
	if raw := c.Query("file_types"); raw != "" {
		for _, part := range strings.Split(raw, ",") {
			if n, err := strconv.Atoi(strings.TrimSpace(part)); err == nil {
				fileTypes = append(fileTypes, n)
			}
		}
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyFileList(ctx, parentID, page, pageSize, orderBy, sortType, fileTypes, resType, dirType, needPlayRecord)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaFileDetail(c *gin.Context) {
	var req struct {
		FileID string `json:"file_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.FileID) == "" {
		fail(c, http.StatusBadRequest, "file_id 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyFileDetail(ctx, req.FileID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaCreateDir(c *gin.Context) {
	var req struct {
		DirName          string `json:"dir_name"`
		ParentID         string `json:"parent_id"`
		FailIfNameExist  bool   `json:"fail_if_name_exist"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.DirName) == "" {
		fail(c, http.StatusBadRequest, "dir_name 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyCreateDir(ctx, req.DirName, req.ParentID, req.FailIfNameExist)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaRename(c *gin.Context) {
	var req struct {
		FileID  string `json:"file_id"`
		NewName string `json:"new_name"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.FileID) == "" || strings.TrimSpace(req.NewName) == "" {
		fail(c, http.StatusBadRequest, "file_id 与 new_name 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyRename(ctx, req.FileID, req.NewName)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaMove(c *gin.Context) {
	var req struct {
		FileIDs  []string `json:"file_ids"`
		ParentID string   `json:"parent_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.FileIDs) == 0 {
		fail(c, http.StatusBadRequest, "file_ids 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.svc.GyMove(ctx, req.FileIDs, req.ParentID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaCopy(c *gin.Context) {
	var req struct {
		FileIDs  []string `json:"file_ids"`
		ParentID string   `json:"parent_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.FileIDs) == 0 {
		fail(c, http.StatusBadRequest, "file_ids 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.svc.GyCopy(ctx, req.FileIDs, req.ParentID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaDelete(c *gin.Context) {
	var req struct {
		FileIDs []string `json:"file_ids"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.FileIDs) == 0 {
		fail(c, http.StatusBadRequest, "file_ids 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.svc.GyDelete(ctx, req.FileIDs)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaRecycle(c *gin.Context) {
	var req struct {
		FileIDs []string `json:"file_ids"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if len(req.FileIDs) == 0 {
		fail(c, http.StatusBadRequest, "file_ids 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.svc.GyRecycle(ctx, req.FileIDs)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaClearRecycle(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	res, err := h.svc.GyClearRecycleBin(ctx)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaDownloadURL(c *gin.Context) {
	var req struct {
		FileID string `json:"file_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.FileID) == "" {
		fail(c, http.StatusBadRequest, "file_id 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyDownloadURL(ctx, req.FileID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

func (h *Handlers) handleGuangyaTaskStatus(c *gin.Context) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if bindJSON(c, &req) != nil {
		return
	}
	if strings.TrimSpace(req.TaskID) == "" {
		fail(c, http.StatusBadRequest, "task_id 不能为空")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	res, err := h.svc.GyTaskStatus(ctx, req.TaskID)
	if err != nil {
		fail(c, http.StatusBadRequest, err.Error())
		return
	}
	ok(c, res)
}

// ---------------- 影视发现 ----------------

func (h *Handlers) handleDiscovery(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	items, total := h.svc.Discovery(ctx, service.DiscoveryQuery{
		Source:   c.Query("source"),
		Keyword:  c.Query("keyword"),
		Category: c.Query("category"),
		Genre:    c.Query("genre"),
		Region:   c.Query("region"),
		Year:     c.Query("year"),
		Sort:     c.Query("sort"),
		Page:     queryInt(c, "page", 1),
	})
	ok(c, gin.H{"items": items, "total": total})
}

func (h *Handlers) handleTrending(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	items, sources := h.svc.Trending(ctx, c.Query("platform"), c.Query("chart"), c.Query("region"))
	charts := []map[string]string{}
	for _, ch := range []struct{ Key, Name string }{
		{"all", "全部榜单"}, {"imdb_movie", "IMDb 最热门电影"}, {"imdb_tv", "IMDb 最热门剧集"},
		{"tmdb_trending", "TMDB 趋势"}, {"anilist", "AniList 热门"}, {"bangumi_daily", "Bangumi 今日动漫"},
	} {
		charts = append(charts, map[string]string{"key": ch.Key, "name": ch.Name})
	}
	ok(c, gin.H{"items": items, "sources": sources, "charts": charts})
}

func (h *Handlers) handleCalendar(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	date := time.Now()
	if v := c.Query("date"); v != "" {
		if t, err := time.Parse("2006-01-02", v); err == nil {
			date = t
		}
	}
	days := h.svc.Calendar(ctx, date, c.Query("kind"))
	ok(c, gin.H{"days": days, "week": []string{"周一", "周二", "周三", "周四", "周五", "周六", "周日"}})
}

// handleMetaSources 返回影视探索/榜单的筛选项元数据。
func (h *Handlers) handleMetaSources(c *gin.Context) {
	ok(c, gin.H{
		"discovery_sources": []map[string]string{
			{"key": "tmdb", "name": "TMDB 片库"},
			{"key": "douban", "name": "豆瓣"},
			{"key": "anilist", "name": "AniList 动漫"},
			{"key": "bangumi", "name": "Bangumi 动漫"},
			{"key": "actor", "name": "热门演员"},
		},
		"categories": []string{"全部", "电影", "剧集"},
		"genres":     []string{"全部", "剧情", "科幻", "动画", "悬疑", "动作", "爱情", "奇幻", "历史", "战争", "惊悚", "喜剧", "犯罪", "家庭", "传记", "纪录片"},
		"regions":    []string{"全部", "华语", "美国", "日本", "韩国", "中国大陆", "中国台湾", "中国香港", "英国", "印度"},
		"years":      []string{"全部", "2025", "2024", "2023", "2022", "2021", "2020", "2019", "2010年代", "00年代"},
		"sorts":      []string{"评分", "热度", "年份"},
	})
}

// queryInt 读取整型查询参数。
func queryInt(c *gin.Context, key string, def int) int {
	if v := c.Query(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
