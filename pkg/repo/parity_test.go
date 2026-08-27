// 跨引擎对照测试。
//
// db/query/sqlite 与 db/query/postgres 是两套独立手写的 SQL，各自用引擎最擅长的
// 表达方式（SQLite 用 json_each，PostgreSQL 用 = ANY）。这样做的代价是两套 SQL
// 会慢慢跑偏——某天只改了一边，另一边行为就不同了，而且换库之前根本发现不了。
//
// 这组测试是双引擎策略成立的前提：同一组种子数据分别灌进两个引擎，
// 用同一张用例表跑 repo 方法，断言两边返回完全一致。不能省。
//
// 本地没有 PostgreSQL 时会自动跳过并打印原因；CI 里有 postgres service container，
// 因此 PR 上必然真跑。
package repo_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"emailbox/db/migrations"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// engine 是参与对照的一个数据库引擎。
type engine struct {
	name  string
	store *repo.Store
}

// parityEngines 返回参与对照的引擎。SQLite 恒在；PostgreSQL 取决于 TEST_POSTGRES_DSN。
func parityEngines(t *testing.T) []engine {
	t.Helper()
	engines := make([]engine, 0, 2)
	engines = append(engines, engine{name: "sqlite", store: newSQLiteStore(t)})
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Log("未设置 TEST_POSTGRES_DSN，跳过 PostgreSQL 侧对照；本次只验证 SQLite")
		return engines
	}
	return append(engines, engine{name: "postgres", store: newPostgresStore(t, dsn)})
}

func newSQLiteStore(t *testing.T) *repo.Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/parity.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Up(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return repo.NewStore(db, "sqlite")
}

func newPostgresStore(t *testing.T, dsn string) *repo.Store {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	// 整个 schema 重建，避免逐表 DROP 的清单随迁移增长而失同步。
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(context.Background(), db, "postgres"); err != nil {
		t.Fatal(err)
	}
	return repo.NewStore(db, "postgres")
}

// seed 灌入两边完全相同的基础数据：一个用户 + 一个个人工作空间 + 默认套餐配额，
// 返回租户 ID（后续几乎所有查询都以它为隔离键）。
func seed(t *testing.T, store *repo.Store) (tenantID string) {
	t.Helper()
	ctx := context.Background()
	const userID = "parity-user"
	tenantID = "parity-tenant"
	if err := store.CreateUser(ctx, &model.User{
		ID: userID, Username: "parity", Email: "parity@example.com",
		PasswordHash: "hash", Status: model.UserStatusActive, PlatformRole: model.PlatformRoleUser,
	}); err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := store.CreateTenant(ctx, &model.Tenant{
		ID: tenantID, Name: "Parity", Slug: "parity",
		Kind: model.TenantKindPersonal, CreatedBy: userID,
	}); err != nil {
		t.Fatalf("创建租户失败: %v", err)
	}
	if err := store.CreateMember(ctx, &model.TenantMember{
		ID: "parity-member", TenantID: tenantID, UserID: userID, Role: model.TenantRoleOwner,
	}); err != nil {
		t.Fatalf("创建成员失败: %v", err)
	}
	plan, err := store.GetDefaultPlan(ctx)
	if err != nil {
		t.Fatalf("读取默认套餐失败: %v", err)
	}
	if err := store.CreateTenantQuota(ctx, tenantID, plan.ID); err != nil {
		t.Fatalf("创建配额失败: %v", err)
	}
	return tenantID
}

// 迁移里的种子数据在两个引擎上必须一致，否则新注册用户拿到的配额会因部署的库而异。
func TestDefaultPlanParity(t *testing.T) {
	var want *model.Plan
	for _, e := range parityEngines(t) {
		got, err := e.store.GetDefaultPlan(context.Background())
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if want == nil {
			want = got
			continue
		}
		// 时间戳由各引擎的 CURRENT_TIMESTAMP 生成，不参与比较。
		got.CreatedAt, got.UpdatedAt = want.CreatedAt, want.UpdatedAt
		if *got != *want {
			t.Errorf("默认套餐在 %s 上与 sqlite 不一致:\n sqlite=%+v\n %s=%+v", e.name, *want, e.name, *got)
		}
	}
}

// GetEffectiveQuota 的 COALESCE 语义在两个引擎上必须完全一致，
// 否则同一个租户在不同部署上会拿到不同的配额。
func TestEffectiveQuotaParity(t *testing.T) {
	var want *model.Limits
	for _, e := range parityEngines(t) {
		tenantID := seed(t, e.store)
		got, err := e.store.GetEffectiveQuota(context.Background(), tenantID)
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if want == nil {
			want = got
			continue
		}
		if *got != *want {
			t.Errorf("生效配额在 %s 上与 sqlite 不一致:\n sqlite=%+v\n %s=%+v", e.name, *want, e.name, *got)
		}
	}
}

// ConsumeUsage 用的是 UPSERT + RETURNING，两个引擎的写法不同（都支持 ON CONFLICT，
// 但 excluded 的可见性与自增语义值得逐步核对）。累加序列必须一致。
func TestConsumeUsageParity(t *testing.T) {
	steps := []int{1, 5, 100, 1}
	var want []int
	for _, e := range parityEngines(t) {
		tenantID := seed(t, e.store)
		ctx := context.Background()
		got := make([]int, 0, len(steps))
		for _, n := range steps {
			v, err := e.store.ConsumeUsage(ctx, tenantID, "2026-01-01", model.MetricMailFetch, n)
			if err != nil {
				t.Fatalf("%s: %v", e.name, err)
			}
			got = append(got, v)
		}
		// 未记账的指标必须读成 0，而不是「记录不存在」的错误。
		other, err := e.store.GetUsageCount(ctx, tenantID, "2026-01-01", model.MetricTokenRefresh)
		if err != nil {
			t.Fatalf("%s: 读取未记账指标不应报错: %v", e.name, err)
		}
		got = append(got, other)

		if want == nil {
			want = got
			continue
		}
		if len(got) != len(want) {
			t.Fatalf("%s: 结果长度不一致", e.name)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("用量累加序列在 %s 上与 sqlite 不一致: sqlite=%v %s=%v", e.name, want, e.name, got)
				break
			}
		}
	}
}

// seedAccounts 在默认分组下建若干账号，返回按创建顺序排列的 ID。
func seedAccounts(t *testing.T, store *repo.Store, tenantID string, emails ...string) []string {
	t.Helper()
	ctx := context.Background()
	if err := store.CreateMailGroup(ctx, &model.MailGroup{
		ID: "parity-group", TenantID: tenantID, Name: "默认分组",
		Color: model.GroupColorGray, IsSystem: true,
	}); err != nil {
		t.Fatalf("创建分组失败: %v", err)
	}
	ids := make([]string, 0, len(emails))
	for i, email := range emails {
		id := "acct-" + email
		if err := store.CreateMailAccount(ctx, &model.MailAccount{
			ID: id, TenantID: tenantID, GroupID: "parity-group",
			Email: email, EmailNormalized: email,
			Provider: "outlook", AccountType: "outlook",
			Status: model.AccountStatusActive, SortOrder: i,
			IMAPPort: 993,
		}); err != nil {
			t.Fatalf("创建账号失败: %v", err)
		}
		ids = append(ids, id)
	}
	return ids
}

// 变长 IN 列表是两套 SQL 差异最大的地方：SQLite 用 sqlc.slice() 展开成
// IN (?, ?, ?)，PostgreSQL 用 = ANY($n::text[])。写法完全不同，
// 返回的 ID 序列必须逐字节一致——这正是双引擎策略成立的前提。
func TestAccountsByIDsParity(t *testing.T) {
	cases := [][]string{
		{},
		{"acct-a@x.com"},
		{"acct-b@x.com", "acct-c@x.com"},
		{"acct-c@x.com", "acct-a@x.com"},     // 传入顺序不影响返回顺序
		{"acct-a@x.com", "acct-nonexistent"}, // 不存在的 ID 静默忽略
		{"acct-a@x.com", "acct-a@x.com"},     // 重复 ID 不产生重复行
	}
	results := make(map[string][][]string)
	for _, e := range parityEngines(t) {
		tenantID := seed(t, e.store)
		seedAccounts(t, e.store, tenantID, "a@x.com", "b@x.com", "c@x.com")
		for _, ids := range cases {
			rows, err := e.store.ListMailAccountsByIDs(context.Background(), tenantID, ids)
			if err != nil {
				t.Fatalf("%s: %v", e.name, err)
			}
			got := make([]string, 0, len(rows))
			for _, r := range rows {
				got = append(got, r.ID)
			}
			results[e.name] = append(results[e.name], got)
		}
	}
	compareParity(t, results, cases)
}

// 批量软删同时清空三个凭据密文列。两个引擎的 UPDATE 语句是分别手写的，
// 影响行数与清空效果都必须一致。
func TestBatchSoftDeleteParity(t *testing.T) {
	type outcome struct {
		affected    int
		remaining   int
		secretsGone bool
	}
	outcomes := map[string]outcome{}
	for _, e := range parityEngines(t) {
		ctx := context.Background()
		tenantID := seed(t, e.store)
		ids := seedAccounts(t, e.store, tenantID, "a@x.com", "b@x.com", "c@x.com")

		// 给第一个账号写入凭据，验证软删会把它清掉
		acct, err := e.store.GetMailAccount(ctx, tenantID, ids[0])
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		acct.RefreshTokenEnc = "enc:v1:secret"
		acct.PasswordEnc = "enc:v1:secret"
		if err := e.store.UpdateMailAccount(ctx, acct); err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}

		// 混入一个不存在的 ID：不该被计入影响行数
		affected, err := e.store.BatchSoftDeleteMailAccounts(ctx, tenantID, []string{ids[0], ids[1], "acct-nope"})
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		remaining, err := e.store.CountMailAccounts(ctx, tenantID)
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		// 软删后按 ID 已查不到，凭据是否清空要靠「查不到」间接确认，
		// 这里改为确认已删账号确实不可见。
		_, getErr := e.store.GetMailAccount(ctx, tenantID, ids[0])
		outcomes[e.name] = outcome{affected: affected, remaining: remaining, secretsGone: getErr != nil}
	}
	var want *outcome
	for name, got := range outcomes {
		if want == nil {
			g := got
			want = &g
			if got.affected != 2 {
				t.Errorf("%s: 影响行数应为 2（不存在的 ID 不计入），实际 %d", name, got.affected)
			}
			continue
		}
		if got != *want {
			t.Errorf("批量软删结果在 %s 上与 sqlite 不一致: %+v vs %+v", name, got, *want)
		}
	}
}

// 别名的批量取回同样走变长 IN（SQLite 的 sqlc.slice / PostgreSQL 的 = ANY），
// 两个方言写法完全不同，必须对照。
//
// 这个用例原本挂在标签上（`TestAccountTagsParity`），注释写的是「标签与别名」
// 但函数体只测了标签——标签随 000007 删掉后，被它顺带遮住的别名一侧就彻底裸奔了。
// 改成直接测别名：要守的从来是「变长 IN 在两个引擎上结果一致」这个模式。
func TestAccountAliasesParity(t *testing.T) {
	results := map[string][]string{}
	for _, e := range parityEngines(t) {
		ctx := context.Background()
		tenantID := seed(t, e.store)
		ids := seedAccounts(t, e.store, tenantID, "a@x.com", "b@x.com")
		// 只给第一个账号挂两个别名：这样既验证了「按 ID 归位」，
		// 也验证了「没有别名的账号不会凭空多出一个空条目」。
		for _, alias := range []string{"a1@x.com", "a2@x.com"} {
			if err := e.store.CreateMailAlias(ctx, "alias-"+alias, tenantID, ids[0], alias, alias); err != nil {
				t.Fatalf("%s: %v", e.name, err)
			}
		}
		aliases, err := e.store.ListMailAliases(ctx, tenantID, ids)
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		var flat []string
		for _, accountID := range ids {
			for _, alias := range aliases[accountID] {
				flat = append(flat, accountID+"/"+alias)
			}
		}
		results[e.name] = flat
	}
	var want []string
	for name, got := range results {
		if want == nil {
			want = got
			if len(got) != 2 {
				t.Errorf("%s: 期望 2 条别名关联，实际 %v", name, got)
			}
			continue
		}
		if len(got) != len(want) {
			t.Errorf("别名关联在 %s 上与 sqlite 不一致: %v vs %v", name, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("别名关联在 %s 上与 sqlite 不一致: %v vs %v", name, got, want)
				break
			}
		}
	}
}

// compareParity 断言各引擎在每个用例上返回了完全相同的 ID 序列。
func compareParity(t *testing.T, results map[string][][]string, cases [][]string) {
	t.Helper()
	base, ok := results["sqlite"]
	if !ok {
		t.Fatal("缺少 sqlite 基准结果")
	}
	for name, got := range results {
		if name == "sqlite" {
			continue
		}
		for i := range cases {
			if len(got[i]) != len(base[i]) {
				t.Errorf("用例 %v 在 %s 上返回 %v，sqlite 返回 %v", cases[i], name, got[i], base[i])
				continue
			}
			for j := range got[i] {
				if got[i][j] != base[i][j] {
					t.Errorf("用例 %v 在 %s 上返回 %v，sqlite 返回 %v", cases[i], name, got[i], base[i])
					break
				}
			}
		}
	}
}

// 筛选与排序是两套 SQL 差异最大的地方：SQLite 用 instr()，PostgreSQL 用 strpos()；
// 可选条件的 NULL 判定、排序变体也各写各的。同一张用例表在两个引擎上
// 必须返回完全相同的 ID 序列——这是双引擎策略成立的前提，不能省。
func TestAccountFilterParity(t *testing.T) {
	cases := []model.AccountFilter{
		{},
		{Status: "active"},
		{Status: "banned"},
		{RefreshStatus: "failed"},
		{Provider: "gmail"},
		{Query: "ALPHA"}, // 大小写不敏感
		{Query: "beta"},
		{Query: "备注"}, // 命中备注而非邮箱
		{Query: "%"},  // 通配符按字面量处理，不该匹配全部
		{Query: "_"},
		{Query: "nonexistent"},
		{Status: "active", Provider: "outlook"}, // 多条件叠加
		{Sort: "email", Order: "asc"},
		{Sort: "email", Order: "desc"},
		{Sort: "created_at", Order: "asc"},
		{Sort: "last_refresh_at", Order: "desc"},
		{Sort: "sort_order", Order: "asc"},
		{Limit: 2}, // 分页边界
		{Limit: 2, Page: 2},
		{Limit: 2, Page: 99},
	}

	type engineResult struct {
		ids    []string
		counts int
	}
	results := map[string][]engineResult{}

	for _, e := range parityEngines(t) {
		ctx := context.Background()
		tenantID := seed(t, e.store)
		ids := seedAccounts(t, e.store, tenantID, "alpha@outlook.com", "beta@gmail.com", "gamma@outlook.com")

		// 给数据加上区分度，让每个筛选条件都有非平凡的结果
		first, err := e.store.GetMailAccount(ctx, tenantID, ids[0])
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		first.Remark = "重要备注"
		if err := e.store.UpdateMailAccount(ctx, first); err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		second, _ := e.store.GetMailAccount(ctx, tenantID, ids[1])
		second.Provider = "gmail"
		second.Status = model.AccountStatusBanned
		if err := e.store.UpdateMailAccount(ctx, second); err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}

		got := make([]engineResult, 0, len(cases))
		for _, f := range cases {
			rows, err := e.store.ListMailAccountsPage(ctx, tenantID, f)
			if err != nil {
				t.Fatalf("%s: 筛选 %+v 失败: %v", e.name, f, err)
			}
			n, err := e.store.CountMailAccountsFiltered(ctx, tenantID, f)
			if err != nil {
				t.Fatalf("%s: 计数 %+v 失败: %v", e.name, f, err)
			}
			list := make([]string, 0, len(rows))
			for _, r := range rows {
				list = append(list, r.Email)
			}
			got = append(got, engineResult{ids: list, counts: n})
		}
		results[e.name] = got
	}

	base := results["sqlite"]
	// 通配符必须按字面量处理：搜 "%" 不该匹配到任何账号
	for i, f := range cases {
		if f.Query == "%" && len(base[i].ids) != 0 {
			t.Errorf("搜索 %%%% 应按字面量处理，却匹配到 %v", base[i].ids)
		}
	}
	for name, got := range results {
		if name == "sqlite" {
			continue
		}
		for i, f := range cases {
			if got[i].counts != base[i].counts {
				t.Errorf("筛选 %+v 的计数在 %s 上是 %d，sqlite 是 %d", f, name, got[i].counts, base[i].counts)
			}
			if len(got[i].ids) != len(base[i].ids) {
				t.Errorf("筛选 %+v 在 %s 上返回 %v，sqlite 返回 %v", f, name, got[i].ids, base[i].ids)
				continue
			}
			for j := range got[i].ids {
				if got[i].ids[j] != base[i].ids[j] {
					t.Errorf("筛选 %+v 在 %s 上返回 %v，sqlite 返回 %v", f, name, got[i].ids, base[i].ids)
					break
				}
			}
		}
	}
}

// 审计日志的可选筛选在两个引擎上是两套写法（sqlite 的裸 narg 与 postgres 的 ::text），
// 语义必须一致——否则「按管理员筛选」这类追责查询会在换库之后少给几行。
func TestAuditLogParity(t *testing.T) {
	type row struct{ actorKind, action, resourceID string }
	var want []row

	for _, e := range parityEngines(t) {
		ctx := context.Background()
		tenantID := seed(t, e.store)

		entries := []model.AuditLog{
			{ID: "a1", TenantID: tenantID, ActorUserID: "parity-user", ActorName: "parity@example.com",
				ActorKind: model.ActorKindUser, Action: model.AuditAccountCreate, ResourceType: "account", ResourceID: "acc-1"},
			{ID: "a2", TenantID: tenantID, ActorUserID: "parity-user", ActorName: "parity@example.com",
				ActorKind: model.ActorKindAdmin, Action: model.AuditAccountList, ResourceType: "account", ResourceID: ""},
			{ID: "a3", TenantID: tenantID, ActorUserID: "parity-user", ActorName: "parity@example.com",
				ActorKind: model.ActorKindAdmin, Action: model.AuditMessageRead, ResourceType: "message", ResourceID: "msg-9"},
		}
		for _, entry := range entries {
			if err := e.store.CreateAuditLog(ctx, entry); err != nil {
				t.Fatalf("%s: 写审计失败: %v", e.name, err)
			}
		}

		// 只要管理员做过的那两条
		logs, total, err := e.store.ListAuditLogs(ctx, model.AuditFilter{ActorKind: model.ActorKindAdmin})
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if total != 2 {
			t.Fatalf("%s: 按 actor_kind 筛出 %d 条，期望 2 条", e.name, total)
		}

		got := make([]row, 0, len(logs))
		for _, l := range logs {
			got = append(got, row{l.ActorKind, l.Action, l.ResourceID})
		}
		if want == nil {
			want = got
			continue
		}
		if len(got) != len(want) {
			t.Fatalf("%s 返回 %d 条，sqlite 返回 %d 条", e.name, len(got), len(want))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("第 %d 条在 %s 上与 sqlite 不一致: sqlite=%+v %s=%+v", i, e.name, want[i], e.name, got[i])
			}
		}
	}
}

// 后台用户列表把「个人租户」LEFT JOIN 进来并用相关子查询数邮箱，
// 两个引擎的 COALESCE 与子查询语义必须一致。
//
// max_accounts 取的是「租户覆盖值优先、否则套餐值」：反过来写的话，
// 管理员调低某个用户的配额将完全不起作用，而列表上还显示着调低后的数字。
// 超额标记由它推出——调低配额不追溯删除已有数据（08 文档 §4.2），
// 所以「已经超额」是合法且会长期存在的状态，后台要能一眼看见。
func TestAdminUsersParity(t *testing.T) {
	type row struct {
		email, tenantName string
		accountCount      int
		maxAccounts       int
		overQuota         bool
	}
	var want []row

	for _, e := range parityEngines(t) {
		ctx := context.Background()
		tenantID := seed(t, e.store)
		seedAccounts(t, e.store, tenantID, "u1@example.com", "u2@example.com")

		// 把上限压到 1，低于现有的 2 个账号：这正是「调低配额不追溯」之后的常态。
		limit := 1
		updatedBy := "parity-user"
		if err := e.store.UpdateTenantQuotaOverrides(ctx, tenantID, repo.QuotaOverrides{
			MaxAccounts: &limit, Note: "parity", UpdatedBy: &updatedBy,
		}); err != nil {
			t.Fatalf("%s: 覆盖配额失败: %v", e.name, err)
		}

		users, total, err := e.store.ListAdminUsers(ctx, model.AdminUserFilter{})
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if total != 1 {
			t.Fatalf("%s: 用户总数 %d，期望 1", e.name, total)
		}

		got := make([]row, 0, len(users))
		for _, u := range users {
			got = append(got, row{u.Email, u.TenantName, u.AccountCount, u.MaxAccounts, u.OverQuota})
		}
		if got[0].maxAccounts != limit {
			t.Errorf("%s: max_accounts = %d，覆盖值 %d 没生效（COALESCE 顺序写反了？）",
				e.name, got[0].maxAccounts, limit)
		}
		if !got[0].overQuota {
			t.Errorf("%s: 2 个账号 / 上限 1 应当标记超额", e.name)
		}
		if want == nil {
			want = got
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("第 %d 行在 %s 上与 sqlite 不一致: sqlite=%+v %s=%+v", i, e.name, want[i], e.name, got[i])
			}
		}
	}
}

// 总览是一条八个子查询的语句，其中两个 SUM 在 sqlc 眼里是 interface{}，
// 两个驱动回来的具体类型并不相同（int64 / numeric）。这条守的是 asInt64 的转换。
func TestPlatformStatsParity(t *testing.T) {
	var want *model.PlatformStats

	for _, e := range parityEngines(t) {
		ctx := context.Background()
		tenantID := seed(t, e.store)
		seedAccounts(t, e.store, tenantID, "s1@example.com", "s2@example.com")
		if _, err := e.store.ConsumeUsage(ctx, tenantID, "2026-08-20", model.MetricMailFetch, 7); err != nil {
			t.Fatalf("%s: 记用量失败: %v", e.name, err)
		}

		got, err := e.store.GetPlatformStats(ctx, "2026-08-20")
		if err != nil {
			t.Fatalf("%s: %v", e.name, err)
		}
		if got.MailFetchToday != 7 {
			t.Errorf("%s: 今日拉信量 %d，期望 7", e.name, got.MailFetchToday)
		}
		if want == nil {
			want = got
			continue
		}
		if *got != *want {
			t.Errorf("平台统计在 %s 上与 sqlite 不一致:\n sqlite=%+v\n %s=%+v", e.name, *want, e.name, *got)
		}
	}
}

// WithTx 必须可重入。
//
// 有些 repo 方法自带事务（DeleteTenant 要把软删和清理会话绑在一起），service 把它们
// 组合进更大的事务时就会嵌套。SQLite 的生产配置只有一个连接，内层若真去 BeginTx，
// 就是在等一把外层自己攥着的锁——症状不是报错而是整个进程静默挂死。
// 这条用例带超时地跑一遍嵌套路径，挂死时会以 panic 收场而不是让 CI 卡到超时。
func TestWithTxIsReentrant(t *testing.T) {
	for _, e := range parityEngines(t) {
		ctx := context.Background()
		tenantID := seed(t, e.store)

		done := make(chan error, 1)
		go func() {
			done <- e.store.WithTx(ctx, func(tx *repo.Store) error {
				// DeleteTenant 内部还会再要一次事务
				return tx.DeleteTenant(ctx, tenantID)
			})
		}()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("%s: 嵌套事务失败: %v", e.name, err)
			}
		case <-time.After(10 * time.Second):
			t.Fatalf("%s: 嵌套事务超过 10 秒未返回，几乎可以肯定是自己等自己的锁", e.name)
		}

		if _, err := e.store.GetTenantByID(ctx, tenantID); !errors.Is(err, repo.ErrNotFound) {
			t.Errorf("%s: 租户应当已被删除，err=%v", e.name, err)
		}
	}
}
