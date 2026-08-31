package api

import (
	"emailbox/pkg/handler"
	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"
	"net/http"
	"strings"
	"time"

	"github.com/labstack/echo/v5"
	echomw "github.com/labstack/echo/v5/middleware"
)

// 请求体大小限制。全局用 DefaultBodyLimit 保护未鉴权接口，
// 导入类接口放宽到 BulkBodyLimit——8MB 约够 10 万行导入。
const (
	DefaultBodyLimit int64 = 64 * 1024
	BulkBodyLimit    int64 = 8 * 1024 * 1024
)

// GlobalBodyLimit 是全局请求体上限。未鉴权接口在鉴权之前就会读取并解析请求体，
// 没有上限时一个超大请求即可耗尽内存（超时只限制时长，不限制体积）。
//
// 大请求体接口必须由这里的 Skipper 放行，再由路由上的 BulkBody 单独约束：
// 路由级的 BodyLimit **不会**覆盖全局的那个——两层都会生效，而请求体先经过全局
// 中间件包装，所以更严的那个说了算。两者必须成对出现，bodylimit_test.go 固定了这个行为。
func GlobalBodyLimit() echo.MiddlewareFunc {
	return echomw.BodyLimitWithConfig(echomw.BodyLimitConfig{
		LimitBytes: DefaultBodyLimit,
		Skipper:    func(c *echo.Context) bool { return IsBulkPath(c.Request().URL.Path) },
	})
}

// BulkBody 供导入、批量操作等大请求路由使用。
func BulkBody() echo.MiddlewareFunc { return echomw.BodyLimit(BulkBodyLimit) }

// IsBulkPath 判断请求是否指向大请求体接口。命名遵循 05 文档 §1 的约定：
// 导入接口以 /import 结尾、导出接口以 /export 结尾，批量操作形如 .../batch/<action>。
// 导出按 ID 选中 5000 个账号时，光 UUID 列表就有 ~180KB，同样超出全局上限。
func IsBulkPath(path string) bool {
	return strings.HasSuffix(path, "/import") || strings.HasSuffix(path, "/export") ||
		strings.Contains(path, "/batch/")
}

type Handlers struct {
	Auth    *handler.AuthHandler
	APIKey  *handler.APIKeyHandler
	User    *handler.UserHandler
	Tenant  *handler.TenantHandler
	Member  *handler.MemberHandler
	Group   *handler.GroupHandler
	Account *handler.AccountHandler
	Quota   *handler.QuotaHandler
	Message *handler.MessageHandler
	Admin   *handler.AdminHandler
	Job     *handler.JobHandler
	Refresh *handler.RefreshHandler
	OAuth   *handler.OAuthHandler
	Audit   *service.AuditService
}

func SetupRoutes(
	e *echo.Echo,
	h Handlers,
	auth *middleware.AuthMiddleware,
	tenant *middleware.TenantMiddleware,
	platform *middleware.PlatformMiddleware,
) {
	// llms.txt 是给 Agent 看的接入说明：公开、无鉴权、不含任何 Key。
	// 挂在根路径是这个文件的惯例（llmstxt.org），抓取方不必先知道 /api/v1。
	e.GET("/llms.txt", h.APIKey.LLMs)

	v1 := e.Group("/api/v1")
	// 未认证的凭据接口按 IP 限流，缓解暴力破解和 bcrypt CPU 消耗。
	// Rate 的单位是「次/秒」：0.2 即约 12 次/分钟，Burst 允许用户连续试错 10 次。
	authLimiter := echomw.RateLimiterWithConfig(echomw.RateLimiterConfig{
		Store: echomw.NewRateLimiterMemoryStoreWithConfig(echomw.RateLimiterMemoryStoreConfig{Rate: 0.2, Burst: 10, ExpiresIn: 15 * time.Minute}),
		// 限流响应也要走统一的 {code,data,message} 结构，否则前端会显示英文原文。
		DenyHandler: func(c *echo.Context, _ string, _ error) error {
			return c.JSON(http.StatusTooManyRequests, map[string]any{"code": 1, "data": nil, "message": "操作过于频繁，请稍后再试"})
		},
	})
	v1.POST("/auth/register", h.Auth.Register, authLimiter)
	v1.POST("/auth/login", h.Auth.Login, authLimiter)
	v1.GET("/oauth/microsoft/callback", h.OAuth.Callback)
	protected := v1.Group("", auth.Require)
	protected.POST("/auth/logout", h.Auth.Logout)
	protected.GET("/auth/session", h.Auth.Session)
	protected.GET("/user/profile", h.User.Get)
	protected.PATCH("/user/profile", h.User.Update)
	protected.POST("/user/change-password", h.User.ChangePassword)
	protected.GET("/tenants", h.Tenant.List)
	protected.POST("/tenants", h.Tenant.Create)
	t := protected.Group("/tenants/:tenantID", tenant.Member)
	t.GET("", h.Tenant.Get, middleware.Require(model.PermissionTenantRead))
	t.PATCH("", h.Tenant.Update, middleware.Require(model.PermissionTenantUpdate))
	t.DELETE("", h.Tenant.Delete, middleware.Require(model.PermissionTenantDelete))
	t.POST("/select", h.Tenant.Select, middleware.Require(model.PermissionTenantRead))
	t.GET("/quota", h.Quota.Get, middleware.Require(model.PermissionTenantRead))
	// API Key 的读与重置都要 tenant:update：它等价于发放一把能读全部邮件的钥匙，
	// 不能让只读成员拿到。Key 自己的角色没有这一项，因此读不到也重置不了自己。
	t.GET("/api-key", h.APIKey.Get, middleware.Require(model.PermissionTenantUpdate))
	t.POST("/api-key/reset", h.APIKey.Reset, middleware.Require(model.PermissionTenantUpdate),
		handler.AuditWrite(h.Audit, model.AuditAPIKeyReset, "api_key", ""))
	t.GET("/members", h.Member.List, middleware.Require(model.PermissionMemberRead))
	t.POST("/members", h.Member.Add, middleware.Require(model.PermissionMemberCreate))
	t.PATCH("/members/:userID", h.Member.Update, middleware.Require(model.PermissionMemberUpdate))
	t.DELETE("/members/:userID", h.Member.Delete, middleware.Require(model.PermissionMemberDelete))

	// 导出限流按**用户**而不是 IP：同一个办公室出口 IP 后面可能有几十个用户，
	// 按 IP 限会互相误伤；而攻击者拿到会话后换 IP 是零成本的，按 IP 也拦不住。
	// 0.17 次/秒 ≈ 10 次/分钟，与 05 文档 §9 的限额一致。
	exportLimiter := echomw.RateLimiterWithConfig(echomw.RateLimiterConfig{
		Store: echomw.NewRateLimiterMemoryStoreWithConfig(echomw.RateLimiterMemoryStoreConfig{Rate: 0.17, Burst: 3, ExpiresIn: 15 * time.Minute}),
		IdentifierExtractor: func(c *echo.Context) (string, error) {
			return middleware.UserID(c), nil
		},
		DenyHandler: func(c *echo.Context, _ string, _ error) error {
			return c.JSON(http.StatusTooManyRequests, map[string]any{"code": 1, "data": nil, "message": "导出过于频繁，请稍后再试"})
		},
	})

	// 邮箱业务。tenantID 来自 URL 且已由 tenant.Member 校验过成员身份。
	mountMailRoutes(t.Group("/mail"), h, exportLimiter)

	// 管理员 API。tenantID 同样来自 URL，但由 RequirePlatformAdmin 决定「谁有权指定它」，
	// 下游 service 与 SQL 完全无感知（08 文档 §2.3）。
	admin := protected.Group("/admin", platform.RequirePlatformAdmin)
	mountAdminRoutes(admin, h, platform, exportLimiter)
}

// mountMailRoutes 注册邮箱业务的全部端点。
//
// 用户侧与管理员侧挂的是**同一份路由表**：05 文档 §12.2 要求
// /admin/tenants/:tenantID/mail/** 与 /tenants/:tenantID/mail/** 完全同构，
// 前端才能原样复用 /mail 的组件。写成一个函数而不是抄两遍，
// 是因为抄两遍必然会在某次改动后只改一边——那种偏差在管理员视图上极难被发现。
func mountMailRoutes(m *echo.Group, h Handlers, exportLimiter echo.MiddlewareFunc) {
	audit := func(action, resourceType, idParam string) echo.MiddlewareFunc {
		return handler.AuditWrite(h.Audit, action, resourceType, idParam)
	}

	m.GET("/groups", h.Group.List, middleware.Require(model.PermissionMailGroupRead))
	// 分组代理的明文只从这一个端点出去，编辑表单打开时取一次。
	// 列表接口不带明文：那样一进 /mail 就把全部分组的代理口令发到浏览器，
	// 而绝大多数时候没人要看它们。
	//
	// 它是读操作，但收敛口径跟着「读走了什么」而不是「谁在读」走，因此按导出
	// 同一档：account:secret（API Key 天然没有这一项）+ **不分角色**的强制审计。
	// 这里用 AuditWrite 而不是 AuditAdminRead 就是为了后者——普通用户取走一条
	// 代理明文同样要留痕。没加限流：一次只出一个分组的一条代理，而分组数本身
	// 受配额约束，与「一次取走整租户凭据」的导出不是一个量级。
	m.GET("/groups/:groupID/proxy", h.Group.Proxy, middleware.Require(model.PermissionAccountSecret),
		audit(model.AuditGroupProxyReveal, "group", "groupID"))
	m.POST("/groups", h.Group.Create, middleware.Require(model.PermissionMailGroupWrite),
		audit(model.AuditGroupWrite, "group", ""))
	m.POST("/groups/reorder", h.Group.Reorder, middleware.Require(model.PermissionMailGroupWrite),
		audit(model.AuditGroupWrite, "group", ""))
	m.PATCH("/groups/:groupID", h.Group.Update, middleware.Require(model.PermissionMailGroupWrite),
		audit(model.AuditGroupWrite, "group", "groupID"))
	m.DELETE("/groups/:groupID", h.Group.Delete, middleware.Require(model.PermissionMailGroupWrite),
		audit(model.AuditGroupWrite, "group", "groupID"))

	// 账号列表是管理员三类必审读操作之一：这是「管理员看了谁的邮箱清单」的唯一痕迹。
	m.GET("/accounts", h.Account.List, middleware.Require(model.PermissionAccountRead),
		handler.AuditAdminRead(h.Audit, model.AuditAccountList, "account", ""))
	m.POST("/accounts", h.Account.Create, middleware.Require(model.PermissionAccountWrite),
		audit(model.AuditAccountCreate, "account", ""))
	// 导入与批量接口的请求体远超全局 64KB 上限，必须配 BulkBody；
	// 全局 BodyLimit 由 IsBulkPath 放行，两者成对出现，缺一半就会返回 413。
	m.POST("/accounts/import", h.Account.Import, middleware.Require(model.PermissionAccountWrite),
		BulkBody(), audit(model.AuditAccountImport, "account", ""))
	// 导出是全平台风险最高的接口：一次调用等于取走整租户的凭据明文。
	// 权限、审计、限流三件必须同时在场，缺任何一件都等于开了一个不留痕或
	// 不设防的凭据出口（07 文档 R9）。
	m.POST("/accounts/export", h.Account.Export, middleware.Require(model.PermissionAccountSecret),
		exportLimiter, BulkBody(), audit(model.AuditAccountExport, "account", ""))
	m.GET("/accounts/:accountID", h.Account.Get, middleware.Require(model.PermissionAccountRead),
		handler.AuditAdminRead(h.Audit, model.AuditAccountRead, "account", "accountID"))
	m.PATCH("/accounts/:accountID", h.Account.Update, middleware.Require(model.PermissionAccountWrite),
		audit(model.AuditAccountUpdate, "account", "accountID"))
	m.DELETE("/accounts/:accountID", h.Account.Delete, middleware.Require(model.PermissionAccountDelete),
		audit(model.AuditAccountDelete, "account", "accountID"))

	m.POST("/accounts/batch/move", h.Account.BatchMove, middleware.Require(model.PermissionAccountWrite),
		BulkBody(), audit(model.AuditAccountBatch, "account", ""))
	m.POST("/accounts/batch/status", h.Account.BatchStatus, middleware.Require(model.PermissionAccountWrite),
		BulkBody(), audit(model.AuditAccountBatch, "account", ""))
	m.POST("/accounts/batch/proxy", h.Account.BatchProxy, middleware.Require(model.PermissionAccountWrite),
		BulkBody(), audit(model.AuditAccountBatch, "account", ""))
	m.POST("/accounts/batch/delete", h.Account.BatchDelete, middleware.Require(model.PermissionAccountDelete),
		BulkBody(), audit(model.AuditAccountDelete, "account", ""))

	// 邮件读写。这些端点每一个都会打上游，耗时以秒计——
	// 批量操作因此限制在 200 封以内，更多的走 P4 的任务系统。
	msg := m.Group("/accounts/:accountID/messages")
	msg.GET("", h.Message.List, middleware.Require(model.PermissionMessageRead))
	// 看正文是第二类必审读操作。列表页只有主题和摘要，点开才是真的读了别人的信。
	msg.GET("/:messageID", h.Message.Detail, middleware.Require(model.PermissionMessageRead),
		handler.AuditAdminRead(h.Audit, model.AuditMessageRead, "message", "messageID"))
	msg.GET("/:messageID/attachments/:attachmentID", h.Message.Attachment,
		middleware.Require(model.PermissionMessageRead),
		handler.AuditAdminRead(h.Audit, model.AuditMessageRead, "message", "messageID"))
	msg.GET("/:messageID/attachments.zip", h.Message.AttachmentsZip,
		middleware.Require(model.PermissionMessageRead),
		handler.AuditAdminRead(h.Audit, model.AuditMessageRead, "message", "messageID"))
	msg.POST("/read", h.Message.MarkRead, middleware.Require(model.PermissionMessageWrite),
		audit(model.AuditMessageWrite, "message", ""))
	msg.POST("/delete", h.Message.Delete, middleware.Require(model.PermissionMessageWrite),
		audit(model.AuditMessageWrite, "message", ""))

	// 令牌刷新与任务。批量提交要扣 daily_token_refresh 配额，因此单独用
	// PermissionTokenRefresh 收敛；查询侧只要 account:read 就够。
	m.POST("/accounts/:accountID/token/refresh", h.Refresh.RefreshOne,
		middleware.Require(model.PermissionTokenRefresh),
		audit(model.AuditTokenRefresh, "account", "accountID"))
	m.POST("/accounts/:accountID/oauth/start", h.OAuth.Start,
		middleware.Require(model.PermissionAccountWrite),
		audit(model.AuditTokenReauthorize, "account", "accountID"))
	m.POST("/accounts/:accountID/oauth/complete", h.OAuth.Complete,
		middleware.Require(model.PermissionAccountWrite),
		audit(model.AuditTokenReauthorize, "account", "accountID"))
	m.POST("/jobs/token-refresh", h.Refresh.SubmitBatch,
		middleware.Require(model.PermissionTokenRefresh), BulkBody(),
		audit(model.AuditJobSubmit, "job", ""))
	m.GET("/refresh/stats", h.Refresh.Stats, middleware.Require(model.PermissionAccountRead))
	m.GET("/refresh/logs", h.Refresh.Logs, middleware.Require(model.PermissionAccountRead))

	m.GET("/jobs", h.Job.List, middleware.Require(model.PermissionAccountRead))
	m.GET("/jobs/:jobID", h.Job.Get, middleware.Require(model.PermissionAccountRead))
	m.GET("/jobs/:jobID/items", h.Job.Items, middleware.Require(model.PermissionAccountRead))
	// SSE。路径以 /stream 结尾，由 IsSSEPath 让 Gzip 中间件跳过——
	// Gzip 会缓冲响应，套在流式接口上表现为「进度条一直不动」。
	m.GET("/jobs/:jobID/stream", h.Job.Stream, middleware.Require(model.PermissionAccountRead))
	m.POST("/jobs/:jobID/stop", h.Job.Stop,
		middleware.Require(model.PermissionTokenRefresh),
		audit(model.AuditJobStop, "job", "jobID"))
}

// mountAdminRoutes 注册 /api/v1/admin 下的端点。整组已经过 RequirePlatformAdmin。
func mountAdminRoutes(admin *echo.Group, h Handlers, platform *middleware.PlatformMiddleware, exportLimiter echo.MiddlewareFunc) {
	admin.GET("/stats", h.Admin.Stats)
	admin.GET("/audit", h.Admin.ListAudit)

	admin.GET("/users", h.Admin.ListUsers)
	admin.GET("/users/:userID", h.Admin.GetUser)
	admin.PATCH("/users/:userID", h.Admin.UpdateUser)
	admin.POST("/users/:userID/reset-password", h.Admin.ResetPassword)
	admin.DELETE("/users/:userID", h.Admin.DeleteUser)

	admin.GET("/plans", h.Admin.ListPlans)
	admin.POST("/plans", h.Admin.CreatePlan)
	admin.PATCH("/plans/:planID", h.Admin.UpdatePlan)
	admin.DELETE("/plans/:planID", h.Admin.DeletePlan)

	// 跨租户视图。TenantContext 确认租户存在，之后的邮箱路由与用户侧完全同构。
	at := admin.Group("/tenants/:tenantID", platform.TenantContext)
	at.GET("/quota", h.Admin.GetTenantQuota)
	at.PATCH("/quota", h.Admin.UpdateTenantQuota)
	mountMailRoutes(at.Group("/mail"), h, exportLimiter)
}
