// Command Tgto 是网盘媒体自动化管理平台的单二进制入口。
//
// 针对小型 NAS 的设计：
//   - 前端 Vue3 构建产物经 go:embed 内嵌，单进程部署，无需 Nginx/Node；
//   - CGO_ENABLED=0 纯静态二进制，可直接交叉编译 linux/amd64 与 linux/arm64；
//   - 默认监听 9058，常驻内存约 15~25MB。
package main

import (
	"context"
	"embed"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"tgto/internal/api"
	"tgto/internal/db"
	"tgto/internal/logger"
	"tgto/internal/service"
)

//go:embed all:web/dist
var embeddedAssets embed.FS

//go:embed VERSION
var versionFile string

// appConfig 运行配置（环境变量驱动）。
type appConfig struct {
	Port      string
	User      string
	Password  string
	DBDir     string
	DataDir   string
	STRMRoot  string
	MemLimit  int64
	Version   string
}

func getenv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// buildVersion 返回内嵌的 VERSION 文件内容（每次构建前由脚本递增补丁号）。
func buildVersion() string {
	if v := strings.TrimSpace(versionFile); v != "" {
		return v
	}
	return "0.0.0"
}

// loadAppConfig 读取环境变量配置。
func loadAppConfig() appConfig {
	// ENV_WEB_PASSPORT：登录用户名（沿用原版命名）；ENV_WEB_PASSWORD：登录密码。
	return appConfig{
		Port:     getenv("PORT", "9058"),
		User:     getenv("ENV_WEB_PASSPORT", "admin"),
		Password: getenv("ENV_WEB_PASSWORD", "123"),
		DBDir:    getenv("TGTO_DB_DIR", "/app/db"),
		DataDir:  getenv("TGTO_DATA_DIR", "/app/data"),
		STRMRoot: getenv("TGTO_STRM_DIR", "/app/strm"),
		MemLimit: parseMemLimit(getenv("GOMEMLIMIT", "")),
		Version:  buildVersion(),
	}
}

// parseMemLimit 解析 GOMEMLIMIT（如 64MiB / 512MiB / 1GiB）。
func parseMemLimit(v string) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(v, "KiB"), strings.HasSuffix(v, "Ki"):
		mult, v = 1024, strings.TrimSuffix(strings.TrimSuffix(v, "iB"), "i")
	case strings.HasSuffix(v, "MiB"), strings.HasSuffix(v, "Mi"):
		mult, v = 1024*1024, strings.TrimSuffix(strings.TrimSuffix(v, "iB"), "i")
	case strings.HasSuffix(v, "GiB"), strings.HasSuffix(v, "Gi"):
		mult, v = 1024*1024*1024, strings.TrimSuffix(strings.TrimSuffix(v, "iB"), "i")
	case strings.HasSuffix(v, "B"):
		mult, v = 1, strings.TrimSuffix(v, "B")
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil {
		return 0
	}
	return n * mult
}

func main() {
	gin.SetMode(gin.ReleaseMode)

	cfg := loadAppConfig()
	if cfg.MemLimit > 0 {
		debug.SetMemoryLimit(cfg.MemLimit)
	}

	ring := logger.NewRingLogger(1000)
	ring.Info(logger.ModSystem, "Tgto v%s 启动中（单二进制内嵌前端，PID=%d）", cfg.Version, os.Getpid())

	// 目录准备（db / strm / data）
	for _, dir := range []string{cfg.DBDir, cfg.STRMRoot, cfg.DataDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			log.Fatalf("创建目录 %s 失败: %v", dir, err)
		}
	}

	database, err := db.Open(cfg.DBDir, ring)
	if err != nil {
		log.Fatalf("数据库初始化失败: %v", err)
	}
	defer func() {
		_ = database.Close()
	}()

	svc := service.NewServices(database, ring, cfg.DataDir, cfg.STRMRoot, cfg.Version)
	svc.ConfigureLimiter(service.Drive115, 2, 4)
	svc.ConfigureLimiter(service.Drive123, 2, 4)
	svc.ConfigureLimiter(service.DriveGY, 2, 4)
	svc.ConfigureLimiter(service.DriveTianYi, 2, 4)

	engine := api.NewRouter(
		svc,
		ring,
		api.StaticConfig{EmbedFS: embeddedAssets},
		cfg.Version,
		cfg.User,
		cfg.Password,
	)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      engine,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 10 * time.Minute,
		IdleTimeout:  120 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// 频道监控调度器
	svc.StartScheduler(ctx)

	// 光鸭 Token 定时自动刷新
	svc.StartGyTokenRefresh(ctx)

	go func() {
		ring.Info(logger.ModSystem, "HTTP 服务已监听 :%s（数据目录 %s）", cfg.Port, filepath.Clean(cfg.DBDir))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			ring.Error(logger.ModSystem, "HTTP 服务异常退出: %v", err)
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	ring.Info(logger.ModSystem, "收到退出信号，正在优雅关闭...")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		ring.Error(logger.ModSystem, "优雅关闭超时: %v", err)
	}
	ring.Info(logger.ModSystem, "Tgto 已安全退出")
}
