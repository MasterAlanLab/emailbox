package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"emailbox/api"
	"emailbox/configs"
	"emailbox/db/migrations"
	"emailbox/pkg/database"
	"emailbox/pkg/handler"
	"emailbox/pkg/job"
	middleware2 "emailbox/pkg/middleware"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
)

func main() {
	// echo 内部使用 log/slog，应用日志保持一致，避免同一份 stdout 里混两种格式。
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := configs.Init(); err != nil {
		fatal("配置初始化失败", err)
	}
	if err := database.Init(); err != nil {
		fatal("数据库初始化失败", err)
	}
	if err := migrations.Up(context.Background(), database.GetDB(), configs.AppConfig.Database.Driver); err != nil {
		_ = database.Close()
		fatal("数据库迁移失败", err)
	}

	store := repo.NewStore(database.GetDB(), configs.AppConfig.Database.Driver)
	platformService := service.NewPlatformService(store)
	// 提权失败不阻断启动：服务本身可用，只是后台进不去，日志里会有 WARN。
	if err := platformService.BootstrapAdmin(context.Background(), configs.AppConfig.SaaS.BootstrapAdminUsername); err != nil {
		slog.Error("初始化平台管理员失败", "error", err)
	}
	authService := service.NewAuthService(store)
	userService := service.NewUserService(store)
	tenantService := service.NewTenantService(store)
	memberService := service.NewMemberService(store)
	quotaService := quota.NewService(store)
	cipher, cipherErr := configs.AppConfig.NewCipher()
	if cipherErr != nil {
		fatal("初始化加密器失败", cipherErr)
	}
	groupService := service.NewGroupService(store, cipher, quotaService)
	accountService := service.NewAccountService(store, cipher, quotaService)
	quotaUsageService := service.NewQuotaService(store, quotaService)
	messageService := service.NewMessageService(store, cipher, quotaService, service.ChainOptions{})
	auditService := service.NewAuditService(store)

	// 任务系统。单实例设计（02 文档 §4.3）：SQLite 部署本来就只能单实例，
	// PostgreSQL 的多实例留到 P6 用 SKIP LOCKED 取件时再说。
	jobManager := job.New(store, job.Config{
		Workers:      configs.AppConfig.Job.Workers,
		AccountDelay: time.Duration(configs.AppConfig.Job.AccountDelayMS) * time.Millisecond,
	})
	refreshService := service.NewRefreshService(store, messageService, quotaService, jobManager)
	jobManager.Register(refreshService)
	jobService := service.NewJobService(store, jobManager)

	// 强杀留下的 running 任务在这里被认出来并标为 interrupted。
	// 不做这一步的话，前端的进度条会对着一个永远不动的任务一直转。
	if n, err := jobManager.ReapStale(context.Background()); err != nil {
		slog.Error("回收中断任务失败", "error", err)
	} else if n > 0 {
		slog.Warn("已回收上次运行遗留的中断任务", "count", n)
	}
	adminService := service.NewAdminService(store, platformService, quotaService)
	handlers := api.Handlers{Auth: handler.NewAuthHandler(authService), User: handler.NewUserHandler(userService), Tenant: handler.NewTenantHandler(tenantService), Member: handler.NewMemberHandler(memberService), Group: handler.NewGroupHandler(groupService), Account: handler.NewAccountHandler(accountService, authService), Quota: handler.NewQuotaHandler(quotaUsageService), Message: handler.NewMessageHandler(messageService), Admin: handler.NewAdminHandler(adminService, auditService, quotaUsageService), Job: handler.NewJobHandler(jobService, refreshService), Refresh: handler.NewRefreshHandler(refreshService), Audit: auditService}
	authMiddleware := middleware2.NewAuthMiddleware(authService)
	tenantMiddleware := middleware2.NewTenantMiddleware(store)
	platformMiddleware := middleware2.NewPlatformMiddleware(store)

	e := echo.New()
	// 限流按客户端 IP 计数。默认只信任连接来源；部署在反向代理后面时必须开启
	// TRUST_PROXY，否则所有用户会被算作同一个 IP，一个人触发限流会波及全部用户。
	if configs.AppConfig.Server.TrustProxy {
		e.IPExtractor = echo.ExtractIPFromXFFHeader()
	} else {
		e.IPExtractor = echo.ExtractIPDirect()
	}
	// RequestID 让访问日志和处理器里的错误日志能通过同一个 id 关联起来。
	e.Use(middleware.RequestID())
	e.Use(middleware.RequestLoggerWithConfig(middleware.RequestLoggerConfig{LogStatus: true, LogURI: true, LogMethod: true, LogLatency: true, LogRequestID: true, HandleError: true, LogValuesFunc: func(_ *echo.Context, v middleware.RequestLoggerValues) error {
		slog.Info("request", "method", v.Method, "uri", v.URI, "status", v.Status, "latency_ms", v.Latency.Milliseconds(), "request_id", v.RequestID)
		return nil
	}}))
	e.Use(middleware.Recover())
	e.Use(api.GlobalBodyLimit())
	// Gzip 会缓冲响应，套在 SSE 上会让任务进度攒够一批才下发，
	// 表现为「进度条一直不动」。流式接口必须跳过。
	e.Use(middleware.GzipWithConfig(middleware.GzipConfig{
		Skipper: func(c *echo.Context) bool { return handler.IsSSEPath(c.Request().URL.Path) },
	}))
	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{AllowOrigins: configs.AppConfig.Server.CORSOrigins, AllowMethods: []string{http.MethodGet, http.MethodPost, http.MethodPatch, http.MethodDelete, http.MethodOptions}, AllowHeaders: []string{echo.HeaderOrigin, echo.HeaderContentType, echo.HeaderAccept}, AllowCredentials: true}))
	api.SetupRoutes(e, handlers, authMiddleware, tenantMiddleware, platformMiddleware)
	setupStaticFiles(e)

	// SIGINT/SIGTERM 触发优雅关机（默认 10 秒宽限），之后统一关闭数据库连接。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	go purgeExpiredSessions(ctx, store)
	go purgeJobEvents(ctx, store)
	sc := echo.StartConfig{Address: configs.AppConfig.GetServerAddress(), BeforeServeFunc: func(s *http.Server) error {
		s.ReadHeaderTimeout = 10 * time.Second
		s.ReadTimeout = 30 * time.Second
		s.WriteTimeout = 30 * time.Second
		s.IdleTimeout = 2 * time.Minute
		return nil
	}}
	// Start 会在收到信号后完成优雅关机再返回，正常关机返回 nil。
	err := sc.Start(ctx, e)
	stop()
	// HTTP 已经停了，再给在跑的任务一点时间收尾。超时也不强等：
	// 没跑完的 item 留在 pending，任务会在下次启动时被标为 interrupted。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	jobManager.Shutdown(shutdownCtx)
	cancelShutdown()
	if closeErr := database.Close(); closeErr != nil {
		slog.Error("关闭数据库失败", "error", closeErr)
	}
	if err != nil {
		fatal("服务器异常退出", err)
	}
}

// fatal 记录错误后退出。不用 log.Fatal 是为了统一走 slog，
// 也避免 os.Exit 跳过已注册的 defer。
func fatal(msg string, err error) {
	slog.Error(msg, "error", err)
	os.Exit(1)
}

// purgeExpiredSessions 定期清理过期会话，否则 sessions 表会无限增长。
func purgeExpiredSessions(ctx context.Context, store *repo.Store) {
	const interval = time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := store.DeleteExpiredSessions(ctx); err != nil && ctx.Err() == nil {
			slog.Error("清理过期会话失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// purgeJobEvents 定期清理过期的任务事件。
// 事件量与「账号数 x 任务次数」成正比，不清理会无上限增长；
// 而它的唯一用途是断线重连时回放最近的进度，超过保留期就没有意义了。
func purgeJobEvents(ctx context.Context, store *repo.Store) {
	const interval = 6 * time.Hour
	retention := time.Duration(configs.AppConfig.Job.EventRetentionDays) * 24 * time.Hour
	if retention <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cutoff := time.Now().Add(-retention)
		if err := store.DeleteJobEventsBefore(ctx, cutoff); err != nil && ctx.Err() == nil {
			slog.Error("清理任务事件失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// staticFilePath 将 URL 路径映射到静态目录内的文件路径。
// 先以根路径清洗，保证 ".." 无法逃出 staticDir（防止路径穿越读取任意文件）。
func staticFilePath(staticDir, urlPath string) string {
	return filepath.Join(staticDir, filepath.Clean("/"+urlPath))
}

// regularFile 判断路径是否为可直接返回的普通文件。
// 目录必须排除：c.File 对目录会回落到其中的 index.html，
// 于是 /assets/.. 这类请求会拿到带一年强缓存的首页。
func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// setupStaticFiles 设置静态文件服务.
func setupStaticFiles(e *echo.Echo) {
	// 静态文件目录
	staticDir := "static"

	// 检查静态文件目录是否存在
	if _, err := os.Stat(staticDir); os.IsNotExist(err) {
		slog.Warn("静态文件目录不存在，跳过静态文件服务", "dir", staticDir)
		return
	}

	// 服务带有哈希的静态资源文件（长期缓存）
	e.GET("/assets/*", func(c *echo.Context) error {
		filePath := staticFilePath(staticDir, c.Request().URL.Path)
		if !regularFile(filePath) {
			return echo.NewHTTPError(http.StatusNotFound, "File not found")
		}
		// 设置强缓存：1年，因为文件名包含哈希值
		c.Response().Header().Set("Cache-Control", "public, max-age=31536000, immutable")

		return c.File(filePath)
	})

	// 服务 favicon（短期缓存）
	e.GET("/favicon.ico", func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "public, max-age=86400") // 1天
		return c.File(filepath.Join(staticDir, "favicon.ico"))
	})

	// 服务网站图标 SVG（长期缓存）
	e.GET("/vite.svg", func(c *echo.Context) error {
		c.Response().Header().Set("Cache-Control", "public, max-age=604800") // 7天
		return c.File(filepath.Join(staticDir, "vite.svg"))
	})

	// 处理SPA路由，所有非API请求都返回index.html
	e.GET("/*", func(c *echo.Context) error {
		path := c.Request().URL.Path

		// 如果是API请求，返回404
		if len(path) >= 4 && path[:4] == "/api" {
			return echo.NewHTTPError(http.StatusNotFound, "API endpoint not found")
		}

		// 检查请求的文件是否存在
		filePath := staticFilePath(staticDir, path)
		if path == "/" {
			filePath = filepath.Join(staticDir, "index.html")
		}

		if regularFile(filePath) {
			// 对于 HTML 文件，使用协商缓存
			if filepath.Ext(filePath) == ".html" {
				c.Response().Header().Set("Cache-Control", "no-cache")
			}

			return c.File(filePath)
		}

		// 文件不存在，返回index.html（SPA路由）
		c.Response().Header().Set("Cache-Control", "no-cache")

		return c.File(filepath.Join(staticDir, "index.html"))
	})
}
