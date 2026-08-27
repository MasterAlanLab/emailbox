package service_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"emailbox/configs"
	"emailbox/db/migrations"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"

	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *repo.Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/test.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Up(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return repo.NewStore(db, "sqlite")
}

// 注册是「用户 + 个人工作空间 + 成员关系 + 配额」四件套，缺一不可。
func TestRegisterCreatesPersonalWorkspaceWithQuota(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	ctx := context.Background()
	result, _, err := service.NewAuthService(store).Register(ctx, model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	tenant := result.Tenants[0]
	if tenant.Kind != model.TenantKindPersonal {
		t.Errorf("注册应创建个人工作空间，实际 kind=%q", tenant.Kind)
	}
	if result.ActiveTenantID == nil || *result.ActiveTenantID != tenant.ID {
		t.Error("登录后 active_tenant_id 应指向个人工作空间")
	}
	if result.User.PlatformRole != model.PlatformRoleUser {
		t.Errorf("新用户平台角色应为 user，实际 %q", result.User.PlatformRole)
	}
	// 没有配额记录的租户，之后每一次配额校验都会失败。
	limits, err := store.GetEffectiveQuota(ctx, tenant.ID)
	if err != nil {
		t.Fatalf("读取生效配额失败: %v", err)
	}
	if limits.PlanCode != "free" {
		t.Errorf("默认套餐应为 free，实际 %q", limits.PlanCode)
	}
}

// 注册只要用户名和密码。邮箱是登录后可填、也可以一直不填的资料字段，
// 因此必须能有多个用户同时不填——旧结构下 users.email 是 NOT NULL UNIQUE，
// 空串只允许存在一个，第二个不填邮箱的人会撞唯一索引（000008 换成了部分唯一索引）。
func TestRegisterWithoutEmail(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	ctx := context.Background()
	auth := service.NewAuthService(store)

	for _, name := range []string{"alice", "bobby"} {
		result, _, err := auth.Register(ctx, model.RegisterRequest{Username: name, Password: "secret12"})
		if err != nil {
			t.Fatalf("%s 不填邮箱注册失败: %v", name, err)
		}
		if result.User.Email != "" {
			t.Errorf("%s 的邮箱应为空，实际 %q", name, result.User.Email)
		}
	}

	// 登录认的是用户名，没有邮箱照样能进。
	if _, _, err := auth.Login(ctx, model.LoginRequest{Username: "alice", Password: "secret12"}); err != nil {
		t.Errorf("没有邮箱的用户应能用用户名登录: %v", err)
	}
	// 密码错了仍要拒绝——别把「没邮箱」误放行成「免密」。
	if _, _, err := auth.Login(ctx, model.LoginRequest{Username: "alice", Password: "wrong"}); err == nil {
		t.Error("密码错误应被拒绝")
	}

	// 填了邮箱的仍然必须唯一，否则等于把这层校验整个丢了。
	if _, _, err := auth.Register(ctx, model.RegisterRequest{Username: "carol", Email: "x@example.com", Password: "secret12"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.Register(ctx, model.RegisterRequest{Username: "dave", Email: "x@example.com", Password: "secret12"}); err == nil {
		t.Error("重复邮箱的注册应失败")
	}
	// 填了但格式不对要拒绝：可选不等于不校验。
	if _, _, err := auth.Register(ctx, model.RegisterRequest{Username: "erin", Email: "not-an-email", Password: "secret12"}); err == nil {
		t.Error("邮箱填了但格式非法应被拒绝")
	}
}

// 邮箱设过之后必须能再清掉。用 string 的话空串会被当成「没传」，
// 于是「设错了想删掉」这条路根本走不通。
func TestProfileEmailCanBeSetAndCleared(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	ctx := context.Background()
	result, _, err := service.NewAuthService(store).Register(ctx,
		model.RegisterRequest{Username: "alice", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	users := service.NewUserService(store)
	id := result.User.ID

	set := "alice@example.com"
	got, err := users.Update(ctx, id, model.UpdateProfileRequest{Email: &set})
	if err != nil || got.Email != set {
		t.Fatalf("设置邮箱失败: %v, email=%q", err, got.Email)
	}
	// nil 表示没传这个字段，不该把已有邮箱抹掉。
	got, err = users.Update(ctx, id, model.UpdateProfileRequest{Username: "alice2"})
	if err != nil || got.Email != set {
		t.Fatalf("未提供 email 时不应改动它: %v, email=%q", err, got.Email)
	}
	empty := ""
	got, err = users.Update(ctx, id, model.UpdateProfileRequest{Email: &empty})
	if err != nil || got.Email != "" {
		t.Fatalf("清空邮箱失败: %v, email=%q", err, got.Email)
	}
}

// 事务中途失败必须整体回滚：留下一个没有租户或没有配额的用户，
// 会导致该账号登录后处处报错且无法自助修复。
func TestRegisterRollsBackOnFailure(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	ctx := context.Background()
	auth := service.NewAuthService(store)

	first, _, err := auth.Register(ctx, model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	// 同名用户会在 CreateUser 之后的唯一索引上失败前就被拒；这里改用
	// 「用户名可用但邮箱已占用」触发事务中段失败：user 插入成功，随后冲突。
	if _, _, err := auth.Register(ctx, model.RegisterRequest{Username: "alice2", Email: "alice@example.com", Password: "secret12"}); err == nil {
		t.Fatal("邮箱重复的注册应失败")
	}
	if _, err := store.GetUserByUsername(ctx, "alice2"); err == nil {
		t.Error("失败的注册留下了用户记录，事务没有回滚")
	}
	// 首个用户的数据不受影响。
	if _, err := store.GetEffectiveQuota(ctx, first.Tenants[0].ID); err != nil {
		t.Errorf("首个用户的配额记录不应受影响: %v", err)
	}
}

func TestRegisterCreatesTenantOwner(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	auth := service.NewAuthService(store)
	result, token, err := auth.Register(context.Background(), model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	if token == "" {
		t.Fatal("empty session token")
	}
	if len(result.Tenants) != 1 {
		t.Fatalf("want one tenant, got %d", len(result.Tenants))
	}
	member, err := store.GetMember(context.Background(), result.Tenants[0].ID, result.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	if member.Role != model.TenantRoleOwner {
		t.Fatalf("want owner, got %s", member.Role)
	}
}

func TestTenantMembershipIsolation(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	auth := service.NewAuthService(store)
	a, _, _ := auth.Register(context.Background(), model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	b, _, _ := auth.Register(context.Background(), model.RegisterRequest{Username: "bobby", Email: "bob@example.com", Password: "secret12"})
	if _, err := store.GetMember(context.Background(), a.Tenants[0].ID, b.User.ID); err == nil {
		t.Fatal("unrelated user unexpectedly belongs to tenant")
	}
}

func TestSoftDeletedTenantBlocksMemberAccess(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	auth := service.NewAuthService(store)
	ctx := context.Background()
	owner, _, err := auth.Register(ctx, model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	tenants := service.NewTenantService(store)
	// 个人工作空间不可删除，这里用一个团队空间做验证。
	team, err := tenants.Create(ctx, owner.User.ID, model.CreateTenantRequest{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := team.ID
	if err := tenants.Delete(ctx, tenantID); err != nil {
		t.Fatal(err)
	}
	// 租户软删除后，成员查询必须失败，否则该租户下的成员管理接口仍然可用。
	if _, err := store.GetMember(ctx, tenantID, owner.User.ID); err == nil {
		t.Fatal("soft-deleted tenant still resolves membership")
	}
}

func TestDeletingTenantClearsSessionActiveTenant(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	auth := service.NewAuthService(store)
	ctx := context.Background()
	owner, token, err := auth.Register(ctx, model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	_, session, err := auth.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	tenants := service.NewTenantService(store)
	// 个人工作空间不可删除，这里用一个团队空间做验证。
	team, err := tenants.Create(ctx, owner.User.ID, model.CreateTenantRequest{Name: "Team"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tenants.Select(ctx, session.ID, owner.User.ID, team.ID); err != nil {
		t.Fatal(err)
	}
	if err := tenants.Delete(ctx, team.ID); err != nil {
		t.Fatal(err)
	}
	// 删除租户后会话不能再指向它，否则 /auth/session 会返回一个不在 tenants 列表中的 ID。
	result, _, err := auth.Session(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	if result.ActiveTenantID != nil {
		t.Fatalf("session still points at deleted tenant %q", *result.ActiveTenantID)
	}
}

func TestPostgresMigrations(t *testing.T) {
	dsn := os.Getenv("TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("TEST_POSTGRES_DSN not set")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// 整个 schema 重建，而不是逐表 DROP：后者每加一张表都要记得同步这份清单，
	// 漏掉一张就会让下次运行在「表已存在」上失败。
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	if err := migrations.Up(context.Background(), db, "postgres"); err != nil {
		t.Fatal(err)
	}
	store := repo.NewStore(db, "postgres")
	u := &model.User{ID: "test-user", Username: "postgres-user", Email: "postgres@example.com", PasswordHash: "hash", Status: model.UserStatusActive}
	if err := store.CreateUser(context.Background(), u); err != nil {
		t.Fatal(err)
	}
}

// 禁用用户后，其已登录的浏览器下一次请求就该失效。
// 只在登录路径上校验 status 是不够的——被禁用的人手里还握着一个有效会话。
func TestDisabledUserSessionIsRejected(t *testing.T) {
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := testStore(t)
	auth := service.NewAuthService(store)
	ctx := context.Background()
	result, token, err := auth.Register(ctx, model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.Session(ctx, token); err != nil {
		t.Fatalf("禁用前会话应有效: %v", err)
	}

	if err := store.UpdateUserStatus(ctx, result.User.ID, model.UserStatusDisabled); err != nil {
		t.Fatal(err)
	}
	if _, _, err := auth.Session(ctx, token); err == nil {
		t.Fatal("被禁用用户的会话仍然有效")
	}
}
