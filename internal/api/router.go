// Package api 实现 Gin RESTful 接口、认证中间件与 go:embed 前端静态资源服务。
package api

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"tgto/internal/logger"
	"tgto/internal/service"
)

// StaticConfig 静态资源配置。
type StaticConfig struct {
	EmbedFS embed.FS
}

// Handlers 接口处理器集合。
type Handlers struct {
	svc          *service.Services
	log          *logger.RingLogger
	version      string
	adminUser    string
	adminPass    string
}

// NewRouter 构建 Gin 引擎与全部路由。
func NewRouter(svc *service.Services, log *logger.RingLogger, static StaticConfig, version, adminUser, adminPass string) *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	r.SetTrustedProxies(nil)

	h := &Handlers{
		svc:       svc,
		log:       log,
		version:   version,
		adminUser: adminUser,
		adminPass: adminPass,
	}

	api := r.Group("/api")
	{
		api.POST("/auth/login", h.handleLogin)
		api.GET("/health", func(c *gin.Context) {
			ok(c, gin.H{"status": "up", "version": version, "time": time.Now().Format(time.RFC3339)})
		})

		secured := api.Group("")
		secured.Use(authRequired(svc))
		{
			// 系统
			secured.GET("/state", h.handleState)
			secured.POST("/auth/logout", h.handleLogout)
			secured.GET("/config/:section", h.handleGetConfig)
			secured.POST("/config/:section", h.handleSaveConfig)

			// 日志中心
			secured.GET("/logs", h.handleLogs)
			secured.POST("/logs/mock", h.handleMockLog)
			secured.GET("/logs/download", h.handleDownloadLog)

			// 监控历史 / 转存
			secured.GET("/transfer/history", h.handleTransferHistory)
			secured.POST("/transfer/submit", h.handleTransferSubmit)

			// 整理历史
			secured.GET("/organize/history", h.handleOrganizeHistory)
			secured.POST("/organize/test", h.handleOrganizeTest)
			secured.POST("/organize/run", h.handleOrganizeRun)
			secured.POST("/organize/rollback", h.handleOrganizeRollback)
			secured.POST("/organize/delete", h.handleOrganizeDelete)
			secured.POST("/organize/clear", h.handleOrganizeClear)

			// STRM
			secured.POST("/strm/generate", h.handleSTRMGenerate)

			// 实用工具
			secured.POST("/util/proxy_test", h.handleProxyTest)
			secured.POST("/util/telegram_test", h.handleTelegramTest)
			secured.POST("/util/tmdb_test", h.handleTMDBTest)
			secured.POST("/util/strm_check", h.handleSTRMCheck)

			// 光鸭云盘短信验证码登录
			secured.GET("/drive/guangya/status", h.handleGuangyaStatus)
			secured.POST("/drive/guangya/sms/send", h.handleGuangyaSendCode)
			secured.POST("/drive/guangya/sms/login", h.handleGuangyaLogin)
			secured.POST("/drive/guangya/token/refresh", h.handleGuangyaRefresh)
			secured.POST("/drive/guangya/token/verify", h.handleGuangyaVerify)
			secured.POST("/drive/guangya/login/clear", h.handleGuangyaClear)

			// 天翼云盘扫码登录
			secured.GET("/drive/tianyi/status", h.handleTianyiStatus)
			secured.POST("/drive/tianyi/qr/start", h.handleTianyiQRStart)
			secured.POST("/drive/tianyi/qr/status", h.handleTianyiQRStatus)
			secured.POST("/drive/tianyi/qr/cancel", h.handleTianyiQRCancel)
			secured.POST("/drive/tianyi/login/clear", h.handleTianyiClear)

			// 天翼云盘文件管理
			secured.GET("/drive/tianyi/files", h.handleTianyiFiles)
			secured.POST("/drive/tianyi/files/mkdir", h.handleTianyiCreateDir)
			secured.POST("/drive/tianyi/files/rename", h.handleTianyiRename)
			secured.POST("/drive/tianyi/files/move", h.handleTianyiMove)
			secured.POST("/drive/tianyi/files/copy", h.handleTianyiCopy)
			secured.POST("/drive/tianyi/files/delete", h.handleTianyiDelete)
			secured.POST("/drive/tianyi/files/recycle/clear", h.handleTianyiClearRecycle)
			secured.POST("/drive/tianyi/files/download_url", h.handleTianyiDownloadURL)

			// 光鸭云盘文件管理
			secured.GET("/drive/guangya/files", h.handleGuangyaFiles)
			secured.POST("/drive/guangya/files/detail", h.handleGuangyaFileDetail)
			secured.POST("/drive/guangya/files/mkdir", h.handleGuangyaCreateDir)
			secured.POST("/drive/guangya/files/rename", h.handleGuangyaRename)
			secured.POST("/drive/guangya/files/move", h.handleGuangyaMove)
			secured.POST("/drive/guangya/files/copy", h.handleGuangyaCopy)
			secured.POST("/drive/guangya/files/delete", h.handleGuangyaDelete)
			secured.POST("/drive/guangya/files/recycle", h.handleGuangyaRecycle)
			secured.POST("/drive/guangya/files/recycle/clear", h.handleGuangyaClearRecycle)
			secured.POST("/drive/guangya/files/download_url", h.handleGuangyaDownloadURL)
			secured.POST("/drive/guangya/files/task_status", h.handleGuangyaTaskStatus)

			// 影视发现
			secured.GET("/discovery", h.handleDiscovery)
			secured.GET("/trending", h.handleTrending)
			secured.GET("/calendar", h.handleCalendar)
			secured.GET("/meta/sources", h.handleMetaSources)
		}
	}

	mountStatic(r, static)
	return r
}

// authRequired 会话认证中间件。
func authRequired(svc *service.Services) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ""
		if auth := c.GetHeader("Authorization"); auth != "" {
			token = strings.TrimSpace(strings.TrimPrefix(auth, "Bearer"))
			token = strings.TrimSpace(token)
		}
		if token == "" {
			token = strings.TrimSpace(c.Query("token"))
		}
		if !svc.CheckSession(token) {
			fail(c, http.StatusUnauthorized, "登录已过期，请重新登录")
			c.Abort()
			return
		}
		c.Set("token", token)
		c.Next()
	}
}

// mountStatic 内嵌前端构建产物，支持 SPA History 路由回退。
func mountStatic(r *gin.Engine, cfg StaticConfig) {
	dist, err := fs.Sub(cfg.EmbedFS, "web/dist")
	if err != nil {
		r.NoRoute(func(c *gin.Context) {
			c.String(http.StatusInternalServerError, "static assets missing")
		})
		return
	}
	fileServer := http.FileServer(http.FS(dist))

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		if strings.HasPrefix(path, "/api/") {
			fail(c, http.StatusNotFound, "接口不存在: "+path)
			return
		}
		clean := strings.TrimPrefix(path, "/")
		if clean == "" {
			clean = "index.html"
		}
		if f, err := dist.Open(clean); err == nil {
			f.Close()
			// 静态资源加长期缓存（Vite 产物带内容哈希）
			if strings.HasPrefix(clean, "assets/") {
				c.Header("Cache-Control", "public, max-age=31536000, immutable")
			}
			fileServer.ServeHTTP(c.Writer, c.Request)
			return
		}
		// SPA 回退：其余路径统一返回 index.html
		c.Request.URL.Path = "/"
		fileServer.ServeHTTP(c.Writer, c.Request)
	})
}

// ---------------- 响应工具 ----------------

func ok(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "ok", "data": data})
}

func fail(c *gin.Context, status int, message string) {
	c.JSON(status, gin.H{"code": status, "message": message, "data": nil})
}

// bindJSON 绑定 JSON 请求体。
func bindJSON(c *gin.Context, v any) error {
	if err := c.ShouldBindJSON(v); err != nil {
		fail(c, http.StatusBadRequest, "请求参数错误: "+err.Error())
		return err
	}
	return nil
}
