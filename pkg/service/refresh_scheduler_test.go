package service_test

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"emailbox/configs"
	"emailbox/db/migrations"
	"emailbox/pkg/crypto"
	"emailbox/pkg/job"
	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
)

// stubRefresher 让令牌交换在进程内直接成功，用例因此不碰网络。
type stubRefresher struct{}

func (stubRefresher) RefreshToken(context.Context, mailer.Credential) error { return nil }

// schedulerStore 按**生产的**连接配置开库：单连接 + busy_timeout。
//
// 不能用 integration_test.go 里的 testStore：那个多连接的库比生产宽松，而这组
// 用例是全 service 包里唯一真的会让 job.Manager 跑起来的——提交完任务，
// 后台 goroutine 立刻开始写库，与用例自己的读撞在一起。宽松的测试环境下它会
// 偶发 SQLITE_BUSY，而生产的单连接反而是排队等待。同 api/routes_test.go。
func schedulerStore(t *testing.T) *repo.Store {
	t.Helper()
	db, err := sql.Open("sqlite",
		"file:"+t.TempDir()+"/scheduler.db?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Up(context.Background(), db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	return repo.NewStore(db, "sqlite")
}

// schedulerFixture 搭一个「一个租户 + 一个分组 + 一个可刷新的 Outlook 账号」的场景。
func schedulerFixture(t *testing.T) (*service.RefreshScheduler, *repo.Store, string, string) {
	t.Helper()
	configs.AppConfig = &configs.Config{Session: configs.SessionConfig{ExpireHour: 24}}
	store := schedulerStore(t)
	ctx := context.Background()

	registered, _, err := service.NewAuthService(store).Register(ctx,
		model.RegisterRequest{Username: "alice", Email: "alice@example.com", Password: "secret12"})
	if err != nil {
		t.Fatal(err)
	}
	tenantID := registered.Tenants[0].ID

	cipher, err := crypto.New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	quotaService := quota.NewService(store)
	accounts := service.NewAccountService(store, cipher, quotaService)
	groups := service.NewGroupService(store, cipher, quotaService)

	group, err := groups.Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "客户 A"})
	if err != nil {
		t.Fatal(err)
	}
	account, err := accounts.Create(ctx, tenantID, model.CreateMailAccountRequest{
		Email: "user@outlook.com", ClientID: "client", RefreshToken: "refresh-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := accounts.Update(ctx, tenantID, account.ID,
		model.UpdateMailAccountRequest{GroupID: &group.ID}); err != nil {
		t.Fatal(err)
	}

	messages := service.NewMessageService(store, cipher, quotaService, service.ChainOptions{})
	jobs := job.New(store, job.Config{Workers: 2, Heartbeat: 200 * time.Millisecond})
	t.Cleanup(func() {
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		jobs.Shutdown(shutdown)
	})
	refresh := service.NewRefreshService(store, messages, quotaService, jobs).
		WithRefresherFactory(func(*model.MailAccount) service.TokenRefresher { return stubRefresher{} })
	jobs.Register(refresh)

	return service.NewRefreshScheduler(store, refresh), store, tenantID, group.ID
}

// setSchedule 直接把分组的周期摆到指定时刻，跳过 GroupService 那层的「从现在起算」。
func setSchedule(t *testing.T, store *repo.Store, tenantID, groupID string, interval int, next time.Time) {
	t.Helper()
	if err := store.UpdateMailGroupSchedule(context.Background(), tenantID, groupID, interval, &next); err != nil {
		t.Fatal(err)
	}
}

func groupNextRefresh(t *testing.T, store *repo.Store, tenantID, groupID string) *time.Time {
	t.Helper()
	g, err := store.GetMailGroup(context.Background(), tenantID, groupID)
	if err != nil {
		t.Fatal(err)
	}
	return g.NextRefreshAt
}

func countJobs(t *testing.T, store *repo.Store, tenantID string) int {
	t.Helper()
	_, total, err := store.ListJobs(context.Background(), tenantID, model.JobFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return total
}

// 停机后不补跑。
//
// 这是整个调度器里最容易写错的一条：直觉写法是 next += interval，而那会让一个
// 停机三天、间隔 6 小时的分组被判定为欠了十二个周期，然后连着补跑十二轮——
// 那不是补进度，那是把服务商直接打到风控。基准必须是 now。
func TestSchedulerDoesNotBackfillAfterDowntime(t *testing.T) {
	scheduler, store, tenantID, groupID := schedulerFixture(t)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	// 60 天前就该刷了，间隔 7 天——按旧值累加会欠八轮。
	setSchedule(t, store, tenantID, groupID, 7*24*60, now.Add(-60*24*time.Hour))

	if err := scheduler.WithClock(func() time.Time { return now }).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}

	if n := countJobs(t, store, tenantID); n != 1 {
		t.Errorf("停机 60 天后应当只补跑一轮，实际提交了 %d 个任务", n)
	}
	next := groupNextRefresh(t, store, tenantID, groupID)
	want := now.Add(7 * 24 * time.Hour)
	if next == nil || !next.Equal(want) {
		t.Errorf("下次刷新应当从 now 起算为 %v，实际 %v", want, next)
	}
}

// 租户已有刷新任务在跑时跳过，并且**不推进**周期。
//
// 推进的话，一个用户给多个分组设了同样的间隔时，只有排在最前面的那个会真的被刷：
// 其余每一轮都恰好撞上「忙」然后被推到下个周期，用户看到的是「有几个分组从来
// 没有被定时刷过」。不推进则它们只是排队等前一个任务跑完。
func TestSchedulerSkipsBusyTenantWithoutAdvancing(t *testing.T) {
	scheduler, store, tenantID, groupID := schedulerFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	setSchedule(t, store, tenantID, groupID, 7*24*60, due)

	// 一个还没终结的同类任务，模拟用户刚点了「刷新全部」。
	if err := store.CreateJob(ctx, model.Job{
		ID: "running-job", TenantID: tenantID, Type: model.JobTypeTokenRefresh,
		Trigger: model.JobTriggerManual, Status: model.JobStatusRunning,
		TotalCount: 1, Params: "{}",
	}); err != nil {
		t.Fatal(err)
	}

	if err := scheduler.WithClock(func() time.Time { return now }).Tick(ctx); err != nil {
		t.Fatal(err)
	}

	if n := countJobs(t, store, tenantID); n != 1 {
		t.Errorf("租户忙时不应再提交任务，任务总数应仍为 1，实际 %d", n)
	}
	next := groupNextRefresh(t, store, tenantID, groupID)
	if next == nil || !next.Equal(due) {
		t.Errorf("被跳过的分组周期不应推进：期望仍是 %v，实际 %v", due, next)
	}
}

// 分组里没有可刷新的账号时不建空任务，但周期照常推进。
//
// 建空任务的话，用户的任务列表每天会多出几条 total=0 的记录，把真正要看的淹掉；
// 不推进周期则会让这个分组每分钟被重新评估一次，日志里每分钟一条。
func TestSchedulerSkipsEmptyGroupButAdvances(t *testing.T) {
	scheduler, store, tenantID, _ := schedulerFixture(t)
	ctx := context.Background()
	cipher, err := crypto.New("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	// 另建一个空分组，fixture 里那个账号不在它下面。
	empty, err := service.NewGroupService(store, cipher, quota.NewService(store)).
		Create(ctx, tenantID, model.CreateMailGroupRequest{Name: "空分组"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	setSchedule(t, store, tenantID, empty.ID, 7*24*60, now.Add(-time.Minute))

	if err := scheduler.WithClock(func() time.Time { return now }).Tick(ctx); err != nil {
		t.Fatal(err)
	}

	if n := countJobs(t, store, tenantID); n != 0 {
		t.Errorf("空分组不应产生任务，实际提交了 %d 个", n)
	}
	next := groupNextRefresh(t, store, tenantID, empty.ID)
	want := now.Add(7 * 24 * time.Hour)
	if next == nil || !next.Equal(want) {
		t.Errorf("空分组的周期仍应推进到 %v，实际 %v", want, next)
	}
}

// 到期的分组会被真的提交，且任务记成 scheduled 触发——刷新日志据此区分
// 「定时跑的」和「有人手点的」，两者的排查方向完全不同。
func TestSchedulerSubmitsScheduledJob(t *testing.T) {
	scheduler, store, tenantID, groupID := schedulerFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	setSchedule(t, store, tenantID, groupID, 7*24*60, now.Add(-time.Minute))

	if err := scheduler.WithClock(func() time.Time { return now }).Tick(ctx); err != nil {
		t.Fatal(err)
	}

	jobs, total, err := store.ListJobs(ctx, tenantID, model.JobFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 {
		t.Fatalf("应当提交一个任务，实际 %d 个", total)
	}
	if jobs[0].Trigger != model.JobTriggerScheduled {
		t.Errorf("任务触发方式应为 scheduled，实际 %q", jobs[0].Trigger)
	}
	if jobs[0].CreatedBy != "" {
		t.Errorf("定时任务没有发起人，created_by 应为空，实际 %q", jobs[0].CreatedBy)
	}
	if jobs[0].TotalCount != 1 {
		t.Errorf("分组下有一个可刷新账号，total_count 应为 1，实际 %d", jobs[0].TotalCount)
	}
}

// 关掉定时（间隔 0）之后，即使 next_refresh_at 还停在过去也不能再被扫到。
func TestSchedulerIgnoresDisabledGroup(t *testing.T) {
	scheduler, store, tenantID, groupID := schedulerFixture(t)
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	setSchedule(t, store, tenantID, groupID, 0, now.Add(-time.Hour))

	if err := scheduler.WithClock(func() time.Time { return now }).Tick(context.Background()); err != nil {
		t.Fatal(err)
	}
	if n := countJobs(t, store, tenantID); n != 0 {
		t.Errorf("关闭定时的分组不应产生任务，实际 %d 个", n)
	}
}

// 用户被管理员禁用之后，他的分组不该继续被定时刷新。
//
// 其它刷新入口都走鉴权，禁用用户在那里已经被 code 1003 挡住了；调度器是这个系统里
// 第一个「没有登录用户也会动」的东西，这道检查没有别的地方可放。少了它，平台会
// 替一个谁都登不进去的账号，每个周期继续打一轮服务商。
func TestSchedulerSkipsDisabledUser(t *testing.T) {
	scheduler, store, tenantID, groupID := schedulerFixture(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 3, 12, 0, 0, 0, time.UTC)
	due := now.Add(-time.Minute)
	setSchedule(t, store, tenantID, groupID, 7*24*60, due)

	user, err := store.GetUserByUsername(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.UpdateUserStatus(ctx, user.ID, model.UserStatusDisabled); err != nil {
		t.Fatal(err)
	}

	if err := scheduler.WithClock(func() time.Time { return now }).Tick(ctx); err != nil {
		t.Fatal(err)
	}

	if n := countJobs(t, store, tenantID); n != 0 {
		t.Errorf("被禁用用户的分组不应产生任务，实际 %d 个", n)
	}
	// 周期也不该被推进：用户重新启用后，这个分组应当立刻就是到期状态。
	next := groupNextRefresh(t, store, tenantID, groupID)
	if next == nil || !next.Equal(due) {
		t.Errorf("被跳过的分组周期不应推进：期望仍是 %v，实际 %v", due, next)
	}
}
