package quota_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"emailbox/db/migrations"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	_ "modernc.org/sqlite"
)

// seedTenant 建一个挂在默认套餐上的租户，返回其 ID。
func seedTenant(t *testing.T) (*repo.Store, string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/quota.db?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := context.Background()
	if err := migrations.Up(ctx, db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	store := repo.NewStore(db, "sqlite")

	const userID, tenantID = "u1", "t1"
	if err := store.CreateUser(ctx, &model.User{ID: userID, Username: "alice", Email: "alice@example.com", PasswordHash: "x", Status: model.UserStatusActive, PlatformRole: model.PlatformRoleUser}); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTenant(ctx, &model.Tenant{ID: tenantID, Name: "T", Slug: "t", Kind: model.TenantKindPersonal, CreatedBy: userID}); err != nil {
		t.Fatal(err)
	}
	plan, err := store.GetDefaultPlan(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CreateTenantQuota(ctx, tenantID, plan.ID); err != nil {
		t.Fatal(err)
	}
	return store, tenantID
}

func TestEffectiveFallsBackToPlanValues(t *testing.T) {
	store, tenantID := seedTenant(t)
	limits, err := quota.NewService(store).Effective(context.Background(), tenantID)
	if err != nil {
		t.Fatal(err)
	}
	if limits.PlanCode != "free" {
		t.Errorf("套餐应为 free，实际 %q", limits.PlanCode)
	}
	if limits.MaxAccounts != 50 {
		t.Errorf("未覆盖时应取套餐值 50，实际 %d", limits.MaxAccounts)
	}
}

func TestCheckAndConsumeAccumulatesThenRejects(t *testing.T) {
	store, tenantID := seedTenant(t)
	svc := quota.NewService(store)
	ctx := context.Background()

	// free 套餐每日 2000 次取件。
	if err := svc.CheckAndConsume(ctx, tenantID, model.MetricMailFetch, 1999); err != nil {
		t.Fatalf("额度内的消费不应失败: %v", err)
	}
	if err := svc.CheckAndConsume(ctx, tenantID, model.MetricMailFetch, 1); err != nil {
		t.Fatalf("刚好用满不应失败: %v", err)
	}
	err := svc.CheckAndConsume(ctx, tenantID, model.MetricMailFetch, 1)
	if !errors.Is(err, quota.ErrQuotaExceeded) {
		t.Fatalf("超额应返回 ErrQuotaExceeded，实际 %v", err)
	}

	// 超额的那次必须整体回滚，不能把用量推过上限——否则今天的额度会被永久“透支”。
	used, err := svc.Usage(ctx, tenantID, model.MetricMailFetch)
	if err != nil {
		t.Fatal(err)
	}
	if used != 2000 {
		t.Errorf("被拒绝的消费不应留下用量，期望 2000，实际 %d", used)
	}
}

// 令牌刷新只记账、没有上限：卡住它，用户看到的不是「今天少刷一点」，
// 而是一批账号集体登录失败。用量页上那个数字仍然要能涨。
func TestRecordCountsWithoutLimit(t *testing.T) {
	store, tenantID := seedTenant(t)
	svc := quota.NewService(store)
	ctx := context.Background()

	for range 3 {
		if err := svc.Record(ctx, tenantID, model.MetricTokenRefresh, 5000); err != nil {
			t.Fatalf("记账不该失败: %v", err)
		}
	}
	used, err := svc.Usage(ctx, tenantID, model.MetricTokenRefresh)
	if err != nil {
		t.Fatal(err)
	}
	if used != 15000 {
		t.Errorf("用量应累加到 15000，实际 %d", used)
	}
}

func TestCheckCount(t *testing.T) {
	cases := []struct {
		name              string
		limit, current, n int
		wantErr           bool
	}{
		{"额度内", 50, 10, 5, false},
		{"刚好用满", 50, 45, 5, false},
		{"超出一个", 50, 46, 5, true},
		{"不限", model.Unlimited, 10000, 5000, false},
	}
	for _, c := range cases {
		err := quota.CheckCount(c.limit, c.current, c.n, "账号")
		if (err != nil) != c.wantErr {
			t.Errorf("%s: CheckCount(%d,%d,%d) = %v，期望出错=%v", c.name, c.limit, c.current, c.n, err, c.wantErr)
		}
		if err != nil && !errors.Is(err, quota.ErrQuotaExceeded) {
			t.Errorf("%s: 错误应可被 ErrQuotaExceeded 识别，实际 %v", c.name, err)
		}
	}
}

// 导入超额时的正确行为是「导入能装下的部分、其余计入 skipped」，
// 而不是整批失败——用户一次粘几千行，因为超 3 个而全失败体验极差。
func TestAllowance(t *testing.T) {
	cases := []struct {
		limit, current, want, expect int
	}{
		{50, 0, 200, 50},
		{50, 30, 200, 20},
		{50, 50, 200, 0},
		{50, 60, 200, 0}, // 管理员调低配额后已超额，不再允许新增
		{50, 0, 10, 10},
		{model.Unlimited, 99999, 200, 200},
	}
	for _, c := range cases {
		if got := quota.Allowance(c.limit, c.current, c.want); got != c.expect {
			t.Errorf("Allowance(%d,%d,%d) = %d，期望 %d", c.limit, c.current, c.want, got, c.expect)
		}
	}
}
