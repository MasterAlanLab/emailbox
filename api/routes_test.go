package api_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"emailbox/api"
	"emailbox/configs"
	"emailbox/db/migrations"
	"emailbox/pkg/crypto"
	"emailbox/pkg/handler"
	"emailbox/pkg/job"
	"emailbox/pkg/mailer"
	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
	_ "modernc.org/sqlite"
)

// testMailClient 决定某个账号的上游用哪个假通道。
// 默认一切正常；邮箱里带 upstreamFailEmailMarker 的账号则一律以 auth_failed 失败，
// 用来验证协议层错误的 HTTP 映射（见 TestUpstreamAuthFailureIsNotUnauthorized）。
// 做成「按账号选」而不是给 newTestServer 加参数，是为了不动其余几十处调用。
func testMailClient(account *model.MailAccount) mailer.Client {
	if account != nil && strings.Contains(account.Email, upstreamFailEmailMarker) {
		return failingMailClient{kind: mailer.ErrKindAuthFailed}
	}
	return stubMailClient{}
}

// testCipher 是接口测试用的固定密钥加密器。
func testCipher(t *testing.T) crypto.Cipher {
	t.Helper()
	c, err := crypto.New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

// 这些用例走完整的 HTTP 栈（路由 → 中间件 → handler → service → repo），
// 覆盖仅靠 service 层测试无法验证的权限矩阵和认证边界。
func newTestServer(t *testing.T) *echo.Echo {
	e, _, _ := newTestServerWithStore(t)
	return e
}

// newTestServerWithStore 额外把 store 与底层连接交出来，供需要直接断言库里状态的用例
// （例如「删除用户后凭据密文列是否真的被清空了」——那些行已经软删，走 repo 查不到）。
func newTestServerWithStore(t *testing.T) (*echo.Echo, *repo.Store, *sql.DB) {
	t.Helper()
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	// 连接参数与生产完全一致（见 configs.sqliteDSN 与 pkg/database）：
	// busy_timeout 兜住瞬时锁争用，MaxOpenConns(1) 让 SQLite 的写天然串行。
	//
	// 一开始这里没限连接数，想着「让并发写真的并发」。结果是任务系统的用例
	// 间歇性超时：多条连接抢锁时，单次查询可能一直等到 busy_timeout 才继续，
	// 几次叠加就超过了用例的等待上限。而生产根本不会出现这种争用——
	// 它只有一条连接，请求在连接池里排队。测试环境比生产更宽松，
	// 测出来的只能是环境自己的问题。
	db, err := sql.Open("sqlite",
		"file:"+t.TempDir()+"/api.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Up(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	store := repo.NewStore(db, "sqlite")
	handlers := api.Handlers{
		Auth:   handler.NewAuthHandler(service.NewAuthService(store)),
		User:   handler.NewUserHandler(service.NewUserService(store)),
		Tenant: handler.NewTenantHandler(service.NewTenantService(store)),
		Member: handler.NewMemberHandler(service.NewMemberService(store)),
		Group:  handler.NewGroupHandler(service.NewGroupService(store, testCipher(t), quota.NewService(store))),
		Account: handler.NewAccountHandler(
			service.NewAccountService(store, testCipher(t), quota.NewService(store)),
			service.NewAuthService(store)),
		Quota: handler.NewQuotaHandler(service.NewQuotaService(store, quota.NewService(store))),
		// 邮件端点接一个假通道：真实回退链要连微软与各家 IMAP，
		// 这里要测的是路由、越权与参数校验，不是协议本身。
		Message: handler.NewMessageHandler(
			service.NewMessageService(store, testCipher(t), quota.NewService(store), service.ChainOptions{}).
				WithChainFactory(testMailClient),
		),
	}
	platformService := service.NewPlatformService(store)
	auditService := service.NewAuditService(store)
	handlers.Audit = auditService

	// 任务系统。心跳与僵尸判定压到秒级，用例才不用等两分钟；
	// 刷新器换成假的，否则每个用例都会去打微软的令牌端点。
	messageService := service.NewMessageService(store, testCipher(t), quota.NewService(store), service.ChainOptions{}).
		WithChainFactory(testMailClient)
	jobManager := job.New(store, job.Config{
		Workers: 4, Heartbeat: 200 * time.Millisecond, StaleAfter: time.Second,
		ProgressEvery: time.Millisecond,
	})
	refreshService := service.NewRefreshService(store, messageService, quota.NewService(store), jobManager).
		WithRefresherFactory(newStubRefresher)
	jobManager.Register(refreshService)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		jobManager.Shutdown(ctx)
	})
	handlers.Job = handler.NewJobHandler(service.NewJobService(store, jobManager), refreshService)
	handlers.Refresh = handler.NewRefreshHandler(refreshService)
	handlers.Admin = handler.NewAdminHandler(
		service.NewAdminService(store, platformService, quota.NewService(store)),
		auditService,
		service.NewQuotaService(store, quota.NewService(store)),
	)

	e := echo.New()
	api.SetupRoutes(e, handlers,
		middleware.NewAuthMiddleware(service.NewAuthService(store)),
		middleware.NewTenantMiddleware(store),
		middleware.NewPlatformMiddleware(store),
	)
	return e, store, db
}

// do 发起一次请求，token 为空表示不带会话 Cookie。
func do(t *testing.T, e *echo.Echo, method, path, token, body string) (int, string) {
	t.Helper()
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, reader)
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: middleware.SessionCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// doRaw 与 do 相同，但返回整个 recorder——需要检查响应头（附件下载）时用它。
// doRaw 与 do 相同但返回整个 recorder，需要检查响应头或原始响应体时用它。
// 只支持 GET：需要检查原始响应的场景（附件下载、SSE 流）都是 GET。
func doRaw(t *testing.T, e *echo.Echo, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, strings.NewReader(""))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: middleware.SessionCookie, Value: token})
	}
	rec := httptest.NewRecorder()
	e.ServeHTTP(rec, req)
	return rec
}

// register 注册一个用户并返回其会话 token 及默认租户 ID。
func register(t *testing.T, e *echo.Echo, username, email string) (token, tenantID string) {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register",
		strings.NewReader(`{"username":"`+username+`","email":"`+email+`","password":"secret12"}`))
	req.Header.Set(echo.HeaderContentType, echo.MIMEApplicationJSON)
	e.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("注册失败: %d %s", rec.Code, rec.Body.String())
	}
	var payload struct {
		Data struct {
			Tenants []struct {
				ID string `json:"id"`
			} `json:"tenants"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == middleware.SessionCookie {
			token = c.Value
		}
	}
	if token == "" {
		t.Fatal("注册响应未设置会话 Cookie")
	}
	return token, payload.Data.Tenants[0].ID
}

// createTenant 创建一个团队工作空间并返回其 ID。
// 注册自动创建的是个人工作空间，它不允许删除或改标识，涉及这两类操作的用例需要团队空间。
func createTenant(t *testing.T, e *echo.Echo, token, name string) string {
	t.Helper()
	status, body := do(t, e, http.MethodPost, "/api/v1/tenants", token, `{"name":"`+name+`","slug":""}`)
	if status != http.StatusOK {
		t.Fatalf("创建租户失败: %d %s", status, body)
	}
	var payload struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	return payload.Data.ID
}

// addMemberAs 让 owner 把某个已注册用户以指定角色加入租户。
func addMemberAs(t *testing.T, e *echo.Echo, ownerToken, tenantID, username string, role model.TenantRole) {
	t.Helper()
	status, body := do(t, e, http.MethodPost, "/api/v1/tenants/"+tenantID+"/members", ownerToken,
		`{"username":"`+username+`","role":"`+string(role)+`"}`)
	if status != http.StatusOK {
		t.Fatalf("添加成员失败: %d %s", status, body)
	}
}

func TestUnauthenticatedRequestsAreRejected(t *testing.T) {
	e := newTestServer(t)
	for _, path := range []string{"/api/v1/tenants", "/api/v1/user/profile", "/api/v1/auth/session"} {
		if status, _ := do(t, e, http.MethodGet, path, "", ""); status != http.StatusUnauthorized {
			t.Errorf("%s 无会话时应返回 401，实际 %d", path, status)
		}
	}
}

func TestInvalidSessionTokenIsRejected(t *testing.T) {
	e := newTestServer(t)
	if status, _ := do(t, e, http.MethodGet, "/api/v1/tenants", "not-a-real-token", ""); status != http.StatusUnauthorized {
		t.Errorf("伪造 token 应返回 401，实际 %d", status)
	}
}

func TestNonMemberCannotReachTenantEndpoints(t *testing.T) {
	e := newTestServer(t)
	_, tenantID := register(t, e, "alice", "alice@example.com")
	outsider, _ := register(t, e, "mallory", "mallory@example.com")

	// 租户隔离：非成员访问他人租户的任何接口都必须被拒绝。
	cases := []struct{ method, path string }{
		{http.MethodGet, "/api/v1/tenants/" + tenantID},
		{http.MethodGet, "/api/v1/tenants/" + tenantID + "/members"},
		{http.MethodDelete, "/api/v1/tenants/" + tenantID},
	}
	for _, tc := range cases {
		if status, body := do(t, e, tc.method, tc.path, outsider, ""); status != http.StatusForbidden {
			t.Errorf("%s %s 非成员应返回 403，实际 %d %s", tc.method, tc.path, status, body)
		}
	}
}

func TestMemberRoleCannotManageTenantOrMembers(t *testing.T) {
	e := newTestServer(t)
	ownerToken, tenantID := register(t, e, "alice", "alice@example.com")
	memberToken, _ := register(t, e, "bobby", "bob@example.com")
	addMemberAs(t, e, ownerToken, tenantID, "bobby", model.TenantRoleMember)

	// member 只有读权限。
	if status, body := do(t, e, http.MethodGet, "/api/v1/tenants/"+tenantID+"/members", memberToken, ""); status != http.StatusOK {
		t.Errorf("member 应可读取成员列表，实际 %d %s", status, body)
	}
	denied := []struct {
		method, path, body string
	}{
		{http.MethodPatch, "/api/v1/tenants/" + tenantID, `{"name":"hijacked","slug":""}`},
		{http.MethodDelete, "/api/v1/tenants/" + tenantID, ""},
		{http.MethodPost, "/api/v1/tenants/" + tenantID + "/members", `{"username":"x","role":"member"}`},
	}
	for _, tc := range denied {
		if status, body := do(t, e, tc.method, tc.path, memberToken, tc.body); status != http.StatusForbidden {
			t.Errorf("member 执行 %s %s 应返回 403，实际 %d %s", tc.method, tc.path, status, body)
		}
	}
}

func TestAdminCanManageMembersButNotDeleteTenant(t *testing.T) {
	e := newTestServer(t)
	ownerToken, tenantID := register(t, e, "alice", "alice@example.com")
	adminToken, _ := register(t, e, "carol", "carol@example.com")
	register(t, e, "dave", "dave@example.com")
	addMemberAs(t, e, ownerToken, tenantID, "carol", model.TenantRoleAdmin)

	// admin 可以管理成员。
	if status, body := do(t, e, http.MethodPost, "/api/v1/tenants/"+tenantID+"/members", adminToken,
		`{"username":"dave","role":"member"}`); status != http.StatusOK {
		t.Errorf("admin 应可添加成员，实际 %d %s", status, body)
	}
	// 但删除租户是 owner 专属权限。
	if status, body := do(t, e, http.MethodDelete, "/api/v1/tenants/"+tenantID, adminToken, ""); status != http.StatusForbidden {
		t.Errorf("admin 删除租户应返回 403，实际 %d %s", status, body)
	}
}

func TestOwnerHasFullAccess(t *testing.T) {
	e := newTestServer(t)
	ownerToken, tenantID := register(t, e, "alice", "alice@example.com")
	checks := []struct {
		method, path, body string
	}{
		{http.MethodGet, "/api/v1/tenants/" + tenantID, ""},
		{http.MethodGet, "/api/v1/tenants/" + tenantID + "/members", ""},
		{http.MethodPatch, "/api/v1/tenants/" + tenantID, `{"name":"Renamed","slug":""}`},
	}
	for _, tc := range checks {
		if status, body := do(t, e, tc.method, tc.path, ownerToken, tc.body); status != http.StatusOK {
			t.Errorf("owner 执行 %s %s 应成功，实际 %d %s", tc.method, tc.path, status, body)
		}
	}
	// 删除要在团队空间上验证：注册自动创建的个人工作空间禁止删除。
	teamID := createTenant(t, e, ownerToken, "Team")
	if status, body := do(t, e, http.MethodDelete, "/api/v1/tenants/"+teamID, ownerToken, ""); status != http.StatusOK {
		t.Errorf("owner 删除团队空间应成功，实际 %d %s", status, body)
	}
}

// 个人工作空间是用户唯一的落脚点，删掉它会让账号变成登录后处处 403 且无法自助修复的孤儿。
func TestPersonalTenantCannotBeDeleted(t *testing.T) {
	e := newTestServer(t)
	ownerToken, tenantID := register(t, e, "alice", "alice@example.com")
	if status, body := do(t, e, http.MethodDelete, "/api/v1/tenants/"+tenantID, ownerToken, ""); status == http.StatusOK {
		t.Fatalf("个人工作空间不应被删除，实际 %d %s", status, body)
	}
}
