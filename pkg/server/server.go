// Package server 装配 HTTP 服务：配置、数据库、服务对象、路由与后台协程。
//
// 单独成包是为了让 Web/Docker 入口（仓库根的 main.go）与桌面入口（desktop 子模块）
// 共用同一份装配。两个入口只在「监听哪个地址」和「要不要自动登录」上有区别，
// 其余每一行都必须是同一份代码——抄第二遍意味着以后每加一个中间件、每挂一条路由，
// 都要记得在两个地方各改一次，而漏掉的那一次通常是安全相关的那个。
package server

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"

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
	"emailbox/pkg/webui"
)

// shutdownGrace 是 HTTP 停止后留给在跑任务收尾的时间。
const shutdownGrace = 10 * time.Second

// Options 描述服务以哪种形态启动。零值即现有的 Web / Docker 形态。
type Options struct {
	// Desktop 打开桌面形态：数据目录、首启密钥与本地账号自动登录三件事只在这里生效。
	Desktop bool
	// Address 覆盖配置里的监听地址。桌面版传 "127.0.0.1:0"，由内核分配端口。
	Address string
}

// Server 是装配完成、尚未开始监听的服务。
type Server struct {
	echo      *echo.Echo
	jobs      *job.Manager
	store     *repo.Store
	scheduler *service.RefreshScheduler
	address   string
	// desktop 仅在桌面形态下非空。
	desktop *desktopEntry
}

// errNoFrontend 是桌面版缺少前端产物时的启动错误。
var errNoFrontend = errors.New("二进制内没有前端产物，桌面版无法启动；请用 make build-desktop 构建（它会先构建前端再嵌入）")

// New 装配服务。返回后数据库已连接、迁移已执行、路由已挂好，但还没有开始监听。
//
// 出错时内部已经把数据库关掉了，调用方直接报错退出即可。
func New(opts Options) (*Server, error) {
	if err := initRuntime(opts); err != nil {
		return nil, err
	}
	store := repo.NewStore(database.GetDB(), configs.AppConfig.Database.Driver)
	svc, err := buildServices(store, opts.Desktop)
	if err != nil {
		closeDatabase()
		return nil, err
	}

	s := &Server{jobs: svc.jobs, store: store, scheduler: svc.scheduler, address: listenAddress(opts)}
	if opts.Desktop {
		entry, err := newDesktopEntry(context.Background(), store, svc.auth)
		if err != nil {
			closeDatabase()
			return nil, err
		}
		s.desktop = entry
	}
	s.echo = newEcho(svc, s)
	return s, nil
}

// initRuntime 完成配置、桌面覆盖、数据库连接与迁移。
func initRuntime(opts Options) error {
	if opts.Desktop {
		// 桌面版没有 Vite 兜底，缺前端产物就是一个打不开任何页面的窗口。
		// 放在最前面：这一条不成立时，后面建数据目录、生成密钥都是白做的。
		if !webui.Built() {
			return errNoFrontend
		}
		// 必须在 configs.Init 之前，理由见 prepareDesktopEnv 的注释。
		if err := prepareDesktopEnv(); err != nil {
			return err
		}
	}
	if err := configs.Init(); err != nil {
		return err
	}
	if err := database.Init(); err != nil {
		return err
	}
	if err := migrations.Up(context.Background(), database.GetDB(), configs.AppConfig.Database.Driver); err != nil {
		closeDatabase()
		return err
	}
	return nil
}

// services 是装配出来的、后续还要用到的对象，只在本包内部传递。
type services struct {
	auth       *service.AuthService
	jobs       *job.Manager
	scheduler  *service.RefreshScheduler
	handlers   api.Handlers
	authMW     *middleware2.AuthMiddleware
	tenantMW   *middleware2.TenantMiddleware
	platformMW *middleware2.PlatformMiddleware
}

// buildServices 建出全部服务对象与处理器。
//
// desktop 一路传到 AuthHandler，让会话响应能告诉前端自己跑在桌面形态里。
// 走参数而不是配置全局：那样等于多一个用户可设的 DESKTOP_MODE 环境变量，
// 在 Docker 上被误设就是一个藏掉了退出按钮、谁也退不出去的网页版。
func buildServices(store *repo.Store, desktop bool) (*services, error) {
	authService := service.NewAuthService(store)
	platformService := service.NewPlatformService(store, authService)
	// 引导失败不阻断启动：服务本身可用，只是后台进不去，日志里会有 WARN。
	if err := platformService.BootstrapAdmin(context.Background(),
		configs.AppConfig.SaaS.BootstrapAdminUsername, configs.AppConfig.SaaS.BootstrapAdminPassword); err != nil {
		slog.Error("初始化平台管理员失败", "error", err)
	}
	cipher, err := configs.AppConfig.NewCipher()
	if err != nil {
		return nil, err
	}
	quotaService := quota.NewService(store)
	groupService := service.NewGroupService(store, cipher, quotaService)
	accountService := service.NewAccountService(store, cipher, quotaService)
	messageService := service.NewMessageService(store, cipher, quotaService, service.ChainOptions{
		OAuthClientID: configs.AppConfig.OAuth.ClientID, OAuthClientSecret: configs.AppConfig.OAuth.ClientSecret,
	})
	oauthService := service.NewOAuthService(store, cipher, quotaService, messageService, service.OAuthOptions{
		Enabled: configs.AppConfig.OAuth.Enabled, ClientID: configs.AppConfig.OAuth.ClientID,
		ClientSecret: configs.AppConfig.OAuth.ClientSecret, Tenant: configs.AppConfig.OAuth.Tenant,
		RedirectURI: configs.AppConfig.OAuth.RedirectURI,
	})
	apiKeyService := service.NewAPIKeyService(store, cipher)

	// 任务系统。单实例设计（02 文档 §4.3）：SQLite 部署本来就只能单实例，
	// PostgreSQL 的多实例留到 P6 用 SKIP LOCKED 取件时再说。
	jobManager := job.New(store, job.Config{
		Workers:      configs.AppConfig.Job.Workers,
		AccountDelay: time.Duration(configs.AppConfig.Job.AccountDelayMS) * time.Millisecond,
	})
	refreshService := service.NewRefreshService(store, messageService, quotaService, jobManager)
	jobManager.Register(refreshService)

	// 强杀留下的 running 任务在这里被认出来并标为 interrupted。
	// 不做这一步的话，前端的进度条会对着一个永远不动的任务一直转。
	if n, err := jobManager.ReapStale(context.Background()); err != nil {
		slog.Error("回收中断任务失败", "error", err)
	} else if n > 0 {
		slog.Warn("已回收上次运行遗留的中断任务", "count", n)
	}

	auditService := service.NewAuditService(store)
	adminService := service.NewAdminService(store, platformService, quotaService)
	return &services{
		auth:      authService,
		jobs:      jobManager,
		scheduler: service.NewRefreshScheduler(store, refreshService),
		handlers: api.Handlers{
			Auth: handler.NewAuthHandler(authService, desktop), APIKey: handler.NewAPIKeyHandler(apiKeyService),
			User: handler.NewUserHandler(service.NewUserService(store)), Tenant: handler.NewTenantHandler(service.NewTenantService(store)),
			Member: handler.NewMemberHandler(service.NewMemberService(store)), Group: handler.NewGroupHandler(groupService),
			Account: handler.NewAccountHandler(accountService), Quota: handler.NewQuotaHandler(service.NewQuotaService(store, quotaService)),
			Message: handler.NewMessageHandler(messageService),
			Admin:   handler.NewAdminHandler(adminService, auditService, service.NewQuotaService(store, quotaService)),
			Job:     handler.NewJobHandler(service.NewJobService(store, jobManager), refreshService),
			Refresh: handler.NewRefreshHandler(refreshService),
			OAuth:   handler.NewOAuthHandler(oauthService, configs.AppConfig.OAuth.ReturnURL), Audit: auditService,
		},
		authMW:     middleware2.NewAuthMiddleware(authService, apiKeyService),
		tenantMW:   middleware2.NewTenantMiddleware(store),
		platformMW: middleware2.NewPlatformMiddleware(store),
	}, nil
}

// newEcho 建出 echo 实例并挂好中间件、业务路由与静态资源。
func newEcho(svc *services, s *Server) *echo.Echo {
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
		slog.Info("request", "method", v.Method, "uri", sanitizedLogURI(v.URI), "status", v.Status, "latency_ms", v.Latency.Milliseconds(), "request_id", v.RequestID)
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
	api.SetupRoutes(e, svc.handlers, svc.authMW, svc.tenantMW, svc.platformMW)
	// 自动登录入口只在桌面形态下存在：Web 部署挂上它等于开了一个
	// 「猜中 nonce 就拿到会话」的额外面，而那里根本用不到它。
	if s.desktop != nil {
		e.GET(desktopSessionPath, s.handleDesktopSession)
	}
	setupStaticFiles(e)
	return e
}

// listenAddress 决定实际监听地址。Options.Address 优先于配置。
func listenAddress(opts Options) string {
	if opts.Address != "" {
		return opts.Address
	}
	return configs.AppConfig.GetServerAddress()
}

// DesktopEntryPath 返回桌面窗口应当打开的首个地址（带 nonce 的相对路径）。
// 非桌面形态返回 "/"。
func (s *Server) DesktopEntryPath() string {
	if s.desktop == nil {
		return "/"
	}
	return s.desktop.path()
}

// Run 开始监听并阻塞，直到 ctx 被取消或收到 SIGINT / SIGTERM。
//
// onListen 在监听建立之后、开始服务之前被调用，用于取回实际端口——
// 桌面版监听 127.0.0.1:0，端口由内核分配，只能这样拿回来。可以传 nil。
func (s *Server) Run(ctx context.Context, onListen func(addr net.Addr)) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	go purgeExpiredSessions(ctx, s.store)
	go purgeJobEvents(ctx, s.store)
	go purgeJobs(ctx, s.store)
	go purgeRefreshLogs(ctx, s.store)
	// 定时刷新的调度器。它自己不执行刷新，只在分组的周期到了的时候往
	// jobManager 提交任务，因此和上面几个清理协程一样跟着 ctx 退出即可。
	go s.scheduler.Run(ctx)

	sc := echo.StartConfig{
		Address:          s.address,
		ListenerAddrFunc: onListen,
		// 桌面版的窗口就是界面，终端里的 banner 和端口行没有读者。
		HideBanner: s.desktop != nil,
		HidePort:   s.desktop != nil,
		BeforeServeFunc: func(srv *http.Server) error {
			srv.ReadHeaderTimeout = 10 * time.Second
			srv.ReadTimeout = 30 * time.Second
			srv.WriteTimeout = 30 * time.Second
			srv.IdleTimeout = 2 * time.Minute
			return nil
		},
	}
	// Start 会在收到信号后完成优雅关机再返回，正常关机返回 nil。
	err := sc.Start(ctx, s.echo)
	stop()
	// HTTP 已经停了，再给在跑的任务一点时间收尾。超时也不强等：
	// 没跑完的 item 留在 pending，任务会在下次启动时被标为 interrupted。
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), shutdownGrace)
	s.jobs.Shutdown(shutdownCtx)
	cancelShutdown()
	closeDatabase()
	return err
}

// closeDatabase 关闭数据库连接，失败只记日志——调用点都在退出路径上，
// 这时候再返回一个错误没有任何人能处置它。
func closeDatabase() {
	if err := database.Close(); err != nil {
		slog.Error("关闭数据库失败", "error", err)
	}
}

// sanitizedLogURI 避免把 Microsoft 回调里的短期授权码与 OAuth state 写进访问日志。
// 其它 URI 保留查询参数，便于排查筛选与分页问题；这里只收窄含凭据的那个入口。
//
// 桌面版的自动登录入口同样要收窄：nonce 一旦落进日志，
// 任何能读到日志的人都可以拿它换一个完整会话。
func sanitizedLogURI(uri string) string {
	for _, prefix := range []string{"/api/v1/oauth/microsoft/callback", desktopSessionPath} {
		if strings.HasPrefix(uri, prefix+"?") {
			return prefix
		}
	}
	return uri
}
