package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"
)

// promoteToAdmin 直接改库把用户提成平台管理员。
// 走接口的话需要先有一个管理员，是个鸡生蛋问题——真实部署靠 BOOTSTRAP_ADMIN_USERNAME
// 在启动时完成，测试里直接落库等价且更短。
func promoteToAdmin(t *testing.T, store *repo.Store, username string) string {
	t.Helper()
	ctx := context.Background()
	user, err := store.GetUserByUsername(ctx, username)
	if err != nil {
		t.Fatalf("找不到用户 %s: %v", username, err)
	}
	if err := store.UpdateUserPlatformRole(ctx, user.ID, model.PlatformRoleAdmin); err != nil {
		t.Fatalf("提权失败: %v", err)
	}
	return user.ID
}

// adminEndpoints 列出 /admin 下的全部端点。
// 07 文档 P3 验收第一条要求「每个端点一条测试」，因此这张表必须与
// api/routes.go 的 mountAdminRoutes 保持同步——漏一条，就有一个端点没人验证过越权。
func adminEndpoints(tenantID, userID string) []struct{ method, path string } {
	return []struct{ method, path string }{
		{http.MethodGet, "/api/v1/admin/stats"},
		{http.MethodGet, "/api/v1/admin/audit"},
		{http.MethodGet, "/api/v1/admin/users"},
		{http.MethodGet, "/api/v1/admin/users/" + userID},
		{http.MethodPatch, "/api/v1/admin/users/" + userID},
		{http.MethodPost, "/api/v1/admin/users/" + userID + "/reset-password"},
		{http.MethodDelete, "/api/v1/admin/users/" + userID},
		{http.MethodGet, "/api/v1/admin/plans"},
		{http.MethodPost, "/api/v1/admin/plans"},
		{http.MethodPatch, "/api/v1/admin/plans/plan-1"},
		{http.MethodDelete, "/api/v1/admin/plans/plan-1"},
		{http.MethodGet, "/api/v1/admin/tenants/" + tenantID + "/quota"},
		{http.MethodPatch, "/api/v1/admin/tenants/" + tenantID + "/quota"},
		{http.MethodGet, "/api/v1/admin/tenants/" + tenantID + "/mail/groups"},
		{http.MethodPost, "/api/v1/admin/tenants/" + tenantID + "/mail/groups"},
		{http.MethodGet, "/api/v1/admin/tenants/" + tenantID + "/mail/accounts"},
		{http.MethodPost, "/api/v1/admin/tenants/" + tenantID + "/mail/accounts"},
		{http.MethodPost, "/api/v1/admin/tenants/" + tenantID + "/mail/accounts/import"},
		{http.MethodPost, "/api/v1/admin/tenants/" + tenantID + "/mail/accounts/export"},
		{http.MethodPost, "/api/v1/admin/tenants/" + tenantID + "/mail/accounts/batch/delete"},
		{http.MethodGet, "/api/v1/admin/tenants/" + tenantID + "/mail/accounts/acc-1/messages"},
	}
}

// P3 验收第 1 条：非管理员访问任何 /admin/* 端点均 403。
func TestNonAdminIsRejectedOnEveryAdminEndpoint(t *testing.T) {
	e := newTestServer(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	victimToken, _ := register(t, e, "bob", "bob@example.com")
	_ = victimToken

	for _, ep := range adminEndpoints(tenantID, "some-user") {
		status, body := do(t, e, ep.method, ep.path, token, "{}")
		if status != http.StatusForbidden {
			t.Errorf("%s %s: 普通用户拿到 %d，期望 403（%s）", ep.method, ep.path, status, body)
		}
	}
}

// 未登录同样进不去，而且必须是 401 而不是 403——
// 两者的处置不同：前者该去登录，后者登录了也没用。
func TestUnauthenticatedIsRejectedOnAdminEndpoints(t *testing.T) {
	e := newTestServer(t)
	_, tenantID := register(t, e, "alice", "alice@example.com")

	for _, ep := range adminEndpoints(tenantID, "some-user") {
		status, _ := do(t, e, ep.method, ep.path, "", "{}")
		if status != http.StatusUnauthorized {
			t.Errorf("%s %s: 未认证拿到 %d，期望 401", ep.method, ep.path, status)
		}
	}
}

// 被降级的管理员立刻失去后台权限——中间件每次请求都查库，不吃缓存。
func TestDemotedAdminLosesAccessImmediately(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	token, _ := register(t, e, "root", "root@example.com")
	adminID := promoteToAdmin(t, store, "root")

	if status, body := do(t, e, http.MethodGet, "/api/v1/admin/stats", token, ""); status != http.StatusOK {
		t.Fatalf("管理员应当能看总览: %d %s", status, body)
	}
	if err := store.UpdateUserPlatformRole(context.Background(), adminID, model.PlatformRoleUser); err != nil {
		t.Fatal(err)
	}
	if status, _ := do(t, e, http.MethodGet, "/api/v1/admin/stats", token, ""); status != http.StatusForbidden {
		t.Errorf("降级后拿到 %d，期望 403", status)
	}
}

// P3 验收第 2 条：管理员在别人租户下的操作，审计里 actor 是管理员本人、actor_kind=admin。
//
// 这是整个管理员体系的问责基础：如果这里记的是被查看者，
// 事后就永远分不清「用户自己看的」和「管理员看的」。
func TestAdminCrossTenantAccessIsAudited(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)

	victimToken, victimTenant := register(t, e, "victim", "victim@example.com")
	createAccount(t, e, victimToken, victimTenant,
		`{"email":"target@outlook.com","password":"hunter2","refresh_token":"M.token"}`)

	adminToken, _ := register(t, e, "root", "root@example.com")
	adminID := promoteToAdmin(t, store, "root")

	// 管理员查看他人租户的账号列表——这是必审的三类读操作之一。
	path := "/api/v1/admin/tenants/" + victimTenant + "/mail/accounts"
	rec := doRaw(t, e, path, adminToken)
	if rec.Code != http.StatusOK {
		t.Fatalf("管理员应当能看他人账号列表: %d %s", rec.Code, rec.Body.String())
	}
	logs, total, err := store.ListAuditLogs(context.Background(), model.AuditFilter{
		TenantID: victimTenant, Action: model.AuditAccountList,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("审计里有 %d 条账号列表记录，期望 1 条", total)
	}
	entry := logs[0]
	if entry.ActorUserID != adminID {
		t.Errorf("actor_user_id = %q，期望管理员 %q", entry.ActorUserID, adminID)
	}
	if entry.ActorKind != model.ActorKindAdmin {
		t.Errorf("actor_kind = %q，期望 admin", entry.ActorKind)
	}
	if entry.ActorName != "root" {
		t.Errorf("actor_name = %q，期望管理员用户名", entry.ActorName)
	}
	if entry.TenantID != victimTenant {
		t.Errorf("tenant_id = %q，期望被查看者的租户 %q", entry.TenantID, victimTenant)
	}
}

// 普通用户自己看自己的账号列表不该产生审计——那是量最大的一类请求，
// 记下来只会把真正需要追溯的管理员行为淹掉。
func TestOwnerReadingOwnAccountsIsNotAudited(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")

	if status, _ := do(t, e, http.MethodGet, mailPath(tenantID, "/accounts"), token, ""); status != http.StatusOK {
		t.Fatal("查看自己的账号列表应当成功")
	}
	_, total, err := store.ListAuditLogs(context.Background(), model.AuditFilter{Action: model.AuditAccountList})
	if err != nil {
		t.Fatal(err)
	}
	if total != 0 {
		t.Errorf("普通用户的读产生了 %d 条审计，期望 0 条", total)
	}
}

// 写操作两边都记，靠 actor_kind 区分。这条守住「用户自己的写也留痕」。
func TestOwnerWriteIsAuditedAsUser(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	token, tenantID := register(t, e, "alice", "alice@example.com")
	createAccount(t, e, token, tenantID, `{"email":"mine@outlook.com","password":"pw","refresh_token":"M.t"}`)

	logs, total, err := store.ListAuditLogs(context.Background(), model.AuditFilter{
		TenantID: tenantID, Action: model.AuditAccountCreate,
	})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("创建账号产生 %d 条审计，期望 1 条", total)
	}
	if logs[0].ActorKind != model.ActorKindUser {
		t.Errorf("actor_kind = %q，期望 user", logs[0].ActorKind)
	}
}

// P3 验收第 3 条：禁用用户后其会话立即失效；重新启用后可以再登录。
func TestDisablingUserKillsSessionImmediately(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	victimToken, _ := register(t, e, "victim", "victim@example.com")
	victimID := userIDByName(t, store, "victim")

	adminToken, _ := register(t, e, "root", "root@example.com")
	promoteToAdmin(t, store, "root")

	if status, _ := do(t, e, http.MethodGet, "/api/v1/user/profile", victimToken, ""); status != http.StatusOK {
		t.Fatal("禁用之前应当可用")
	}

	status, body := do(t, e, http.MethodPatch, "/api/v1/admin/users/"+victimID, adminToken,
		`{"status":"disabled"}`)
	if status != http.StatusOK {
		t.Fatalf("禁用失败: %d %s", status, body)
	}

	// 只改 status 而留着 session 的话，这一行会是 200——「禁用」就成了摆设。
	if status, _ := do(t, e, http.MethodGet, "/api/v1/user/profile", victimToken, ""); status != http.StatusUnauthorized {
		t.Errorf("禁用后旧会话拿到 %d，期望 401", status)
	}

	// 禁用期间连登录都不该成功
	loginStatus, _ := do(t, e, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"victim","password":"secret12"}`)
	if loginStatus == http.StatusOK {
		t.Error("被禁用的用户不应当能登录")
	}

	if status, _ := do(t, e, http.MethodPatch, "/api/v1/admin/users/"+victimID, adminToken,
		`{"status":"active"}`); status != http.StatusOK {
		t.Fatal("重新启用失败")
	}
	if status, body := do(t, e, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"victim","password":"secret12"}`); status != http.StatusOK {
		t.Errorf("启用后重新登录失败: %d %s", status, body)
	}
}

// 重置密码要一次性回传临时密码，并且把旧会话全部清掉——
// 换了密码却留着旧 cookie，等于没换。
func TestResetPasswordReturnsTempPasswordAndKillsSessions(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	victimToken, _ := register(t, e, "victim", "victim@example.com")
	victimID := userIDByName(t, store, "victim")

	adminToken, _ := register(t, e, "root", "root@example.com")
	promoteToAdmin(t, store, "root")

	status, body := do(t, e, http.MethodPost,
		"/api/v1/admin/users/"+victimID+"/reset-password", adminToken, "")
	if status != http.StatusOK {
		t.Fatalf("重置密码失败: %d %s", status, body)
	}
	var payload struct {
		Data struct {
			Password string `json:"password"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.Data.Password) < 12 {
		t.Errorf("临时密码太短: %q", payload.Data.Password)
	}

	if status, _ := do(t, e, http.MethodGet, "/api/v1/user/profile", victimToken, ""); status != http.StatusUnauthorized {
		t.Errorf("重置密码后旧会话拿到 %d，期望 401", status)
	}
	if status, _ := do(t, e, http.MethodPost, "/api/v1/auth/login", "",
		`{"username":"victim","password":"`+payload.Data.Password+`"}`); status != http.StatusOK {
		t.Error("临时密码应当可以登录")
	}

	// 审计里绝不能出现密码本身
	logs, _, err := store.ListAuditLogs(context.Background(), model.AuditFilter{Action: model.AuditUserResetPassword})
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 {
		t.Fatalf("重置密码产生 %d 条审计，期望 1 条", len(logs))
	}
	if strings.Contains(logs[0].Details, payload.Data.Password) {
		t.Error("审计 details 里出现了明文临时密码")
	}
}

// P3 验收第 4 条：调低配额后新增被拒，已有账号不受影响，后台显示超额标记。
func TestLoweringQuotaBlocksNewAccountsButKeepsExisting(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	ownerToken, tenantID := register(t, e, "owner", "owner@example.com")
	createAccount(t, e, ownerToken, tenantID, `{"email":"a1@outlook.com","password":"pw","refresh_token":"M.t"}`)
	createAccount(t, e, ownerToken, tenantID, `{"email":"a2@outlook.com","password":"pw","refresh_token":"M.t"}`)

	adminToken, _ := register(t, e, "root", "root@example.com")
	promoteToAdmin(t, store, "root")

	// 把上限压到 1，低于现有的 2 个
	status, body := do(t, e, http.MethodPatch, "/api/v1/admin/tenants/"+tenantID+"/quota", adminToken,
		`{"max_accounts":1,"note":"疑似滥用，临时收紧"}`)
	if status != http.StatusOK {
		t.Fatalf("调整配额失败: %d %s", status, body)
	}

	// 新增被拒，且用 1001 这个业务码告诉前端「是配额问题」
	status, body = do(t, e, http.MethodPost, mailPath(tenantID, "/accounts"), ownerToken,
		`{"email":"a3@outlook.com","password":"pw","refresh_token":"M.t"}`)
	if status != http.StatusForbidden {
		t.Errorf("超额新增拿到 %d，期望 403（%s）", status, body)
	}
	if !strings.Contains(body, `"code":1001`) {
		t.Errorf("超额响应缺少 code=1001: %s", body)
	}

	// 已有账号不追溯删除，照样列得出来
	status, body = do(t, e, http.MethodGet, mailPath(tenantID, "/accounts"), ownerToken, "")
	if status != http.StatusOK {
		t.Fatalf("列表失败: %d %s", status, body)
	}
	if !strings.Contains(body, "a1@outlook.com") || !strings.Contains(body, "a2@outlook.com") {
		t.Error("调低配额不应影响已有账号")
	}

	// 后台的用户列表上要看得见超额标记——那是管理员唯一会看的那份清单
	status, body = do(t, e, http.MethodGet, "/api/v1/admin/users?q=owner@example.com", adminToken, "")
	if status != http.StatusOK {
		t.Fatalf("用户列表失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"over_quota":true`) {
		t.Errorf("用户列表缺少超额标记: %s", body)
	}
}

// 调额必须写原因。三个月后回看时，note 是唯一能说明「为什么这个租户不一样」的东西。
func TestQuotaUpdateRequiresNote(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	_, tenantID := register(t, e, "owner", "owner@example.com")
	adminToken, _ := register(t, e, "root", "root@example.com")
	promoteToAdmin(t, store, "root")

	if status, _ := do(t, e, http.MethodPatch, "/api/v1/admin/tenants/"+tenantID+"/quota", adminToken,
		`{"max_accounts":10}`); status != http.StatusBadRequest {
		t.Errorf("缺 note 时拿到 %d，期望 400", status)
	}
}

// P3 验收第 5 条的后端半边：最后一个管理员不可降级、不可删除。
// 少了这条守卫，一次误操作就会让后台永久锁死，而且没有自助恢复途径。
func TestLastAdminCannotBeDemotedOrDeleted(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	adminToken, _ := register(t, e, "root", "root@example.com")
	adminID := promoteToAdmin(t, store, "root")

	status, body := do(t, e, http.MethodPatch, "/api/v1/admin/users/"+adminID, adminToken,
		`{"platform_role":"user"}`)
	if status != http.StatusConflict {
		t.Errorf("降级最后一个管理员拿到 %d，期望 409（%s）", status, body)
	}

	// 删自己会先撞上「不能对自己执行该操作」，所以再造一个管理员来删他
	otherToken, _ := register(t, e, "second", "second@example.com")
	otherID := promoteToAdmin(t, store, "second")
	_ = otherToken

	if status, _ := do(t, e, http.MethodDelete, "/api/v1/admin/users/"+adminID, adminToken,
		""); status != http.StatusConflict {
		t.Error("管理员不应当能删除自己")
	}
	// 现在有两个管理员了，删掉其中一个是允许的
	if status, body := do(t, e, http.MethodDelete, "/api/v1/admin/users/"+otherID, adminToken,
		""); status != http.StatusOK {
		t.Errorf("有两个管理员时删除其一失败: %d %s", status, body)
	}
}

// 删除用户必须物理清除凭据密文（08 文档 §6 第 6 条）。
// 软删只为「误删可恢复」，而第三方邮箱的凭据不该跟着一个 deleted_at 标记长期留存。
func TestDeletingUserWipesCredentialCiphertext(t *testing.T) {
	e, store, db := newTestServerWithStore(t)
	victimToken, victimTenant := register(t, e, "victim", "victim@example.com")
	createAccount(t, e, victimToken, victimTenant,
		`{"email":"target@outlook.com","password":"hunter2","refresh_token":"M.secret-token"}`)
	victimID := userIDByName(t, store, "victim")

	adminToken, _ := register(t, e, "root", "root@example.com")
	promoteToAdmin(t, store, "root")

	status, body := do(t, e, http.MethodDelete, "/api/v1/admin/users/"+victimID, adminToken, "")
	if status != http.StatusOK {
		t.Fatalf("删除用户失败: %d %s", status, body)
	}
	if !strings.Contains(body, `"deleted_accounts":1`) {
		t.Errorf("响应未报告清理的邮箱数: %s", body)
	}

	// 直接查库：软删之后三个密文列必须已经是空串
	var leftover int
	row := db.QueryRow(`SELECT COUNT(*) FROM mail_accounts
		WHERE tenant_id = ? AND (refresh_token_enc <> '' OR password_enc <> '' OR imap_password_enc <> '')`,
		victimTenant)
	if err := row.Scan(&leftover); err != nil {
		t.Fatal(err)
	}
	if leftover != 0 {
		t.Errorf("删除用户后仍有 %d 行残留凭据密文", leftover)
	}

	// 被删用户的会话也要一起失效
	if status, _ := do(t, e, http.MethodGet, "/api/v1/user/profile", victimToken, ""); status != http.StatusUnauthorized {
		t.Errorf("用户被删后旧会话拿到 %d，期望 401", status)
	}
}

// 套餐还有租户在用时不能删——那些租户的生效配额是「套餐值 COALESCE 覆盖值」，
// 套餐没了就查不出配额，等于这些租户的所有操作全被拒。
func TestPlanInUseCannotBeDeleted(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	register(t, e, "owner", "owner@example.com") // 占用默认套餐
	adminToken, _ := register(t, e, "root", "root@example.com")
	promoteToAdmin(t, store, "root")

	status, body := do(t, e, http.MethodGet, "/api/v1/admin/plans", adminToken, "")
	if status != http.StatusOK {
		t.Fatalf("套餐列表失败: %d %s", status, body)
	}
	var plans struct {
		Data []struct {
			ID        string `json:"id"`
			IsDefault bool   `json:"is_default"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &plans); err != nil {
		t.Fatal(err)
	}
	if len(plans.Data) == 0 {
		t.Fatal("迁移应当种入一个默认套餐")
	}
	if status, _ := do(t, e, http.MethodDelete, "/api/v1/admin/plans/"+plans.Data[0].ID, adminToken,
		""); status != http.StatusConflict {
		t.Error("默认套餐 / 在用套餐不应当能删除")
	}
}

// 管理员传一个不存在的租户要拿 404，而不是一个空列表——
// 「这个租户没有邮箱」和「租户 ID 打错了」在排障时的处置完全不同。
func TestAdminUnknownTenantIs404(t *testing.T) {
	e, store, _ := newTestServerWithStore(t)
	adminToken, _ := register(t, e, "root", "root@example.com")
	promoteToAdmin(t, store, "root")

	if status, _ := do(t, e, http.MethodGet,
		"/api/v1/admin/tenants/no-such-tenant/mail/accounts", adminToken, ""); status != http.StatusNotFound {
		t.Error("不存在的租户应当返回 404")
	}
}

func userIDByName(t *testing.T, store *repo.Store, username string) string {
	t.Helper()
	user, err := store.GetUserByUsername(context.Background(), username)
	if err != nil {
		t.Fatalf("找不到用户 %s: %v", username, err)
	}
	return user.ID
}
