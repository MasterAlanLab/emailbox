package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"
)

// scheduleTick 是扫描周期。写成常量而不是配置项：这个值没有需要按部署调整的
// 理由——真正决定「多久刷一次」的是每个分组自己的间隔，而间隔的下限是一小时，
// 一分钟的扫描精度对它绰绰有余。
const scheduleTick = time.Minute

// scheduleBatchSize 是一轮最多取多少个到期分组。取不完的留到下一轮：
// 它们的 next_refresh_at 还在过去，下一分钟照样会被扫出来。
const scheduleBatchSize = 200

// RefreshScheduler 按分组配置的间隔自动提交令牌刷新任务。
//
// 放在 service 而不是 pkg/job：任务系统按设计不认识分组、账号和邮件协议，
// 它只管调度、计数、落库与事件。把「哪些分组该刷了」塞进去会破坏那条边界。
//
// 单实例设计，与 job.Manager 一致（02 文档 §4.3）。多进程跑起来也不会重复提交
// 任务——repo.ClaimGroupRefresh 用一条条件 UPDATE 抢占——但没有做过多实例验证。
type RefreshScheduler struct {
	store   *repo.Store
	refresh *RefreshService

	// now 供测试注入。定时逻辑的正确性几乎全部体现在「用哪个时刻做基准」上，
	// 靠真实时钟根本测不了那些分支。
	now func() time.Time
}

func NewRefreshScheduler(store *repo.Store, refresh *RefreshService) *RefreshScheduler {
	return &RefreshScheduler{store: store, refresh: refresh, now: time.Now}
}

// WithClock 供测试注入时钟，同 RefreshService.WithRefresherFactory 的用法。
func (s *RefreshScheduler) WithClock(now func() time.Time) *RefreshScheduler {
	s.now = now
	return s
}

// Run 阻塞跑调度循环，直到 ctx 取消。由 main 起一个 goroutine 调用。
//
// 第一轮在一个 tick 之后而不是立刻：启动时 ReapStale 正在把僵尸任务标为
// interrupted，此刻就去查「租户有没有任务在跑」会读到那些还没被回收的行。
func (s *RefreshScheduler) Run(ctx context.Context) {
	ticker := time.NewTicker(scheduleTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if err := s.Tick(ctx); err != nil && ctx.Err() == nil {
			slog.Error("定时刷新扫描失败", "error", err)
		}
	}
}

// Tick 处理一轮到期的分组。导出是为了测试能直接驱动一轮，不必等真实时钟。
func (s *RefreshScheduler) Tick(ctx context.Context) error {
	now := s.now()
	groups, err := s.store.ListGroupsDueForRefresh(ctx, now, scheduleBatchSize)
	if err != nil {
		return err
	}
	// busy 记住这一轮里已经判定为「忙」的租户。一个用户常常给多个分组设同样的
	// 间隔，于是它们会在同一轮一起到期，而忙闲状态在这一轮内不会再变回去。
	busy := make(map[string]bool)
	for _, g := range groups {
		if err := s.tickGroup(ctx, g, now, busy); err != nil {
			// 一个分组出错不该让整轮停下：剩下的分组多半属于别的租户，
			// 让它们跟着一起停，一个租户的问题就变成了全平台的定时刷新失效。
			slog.Error("提交定时刷新任务失败",
				"tenant_id", g.TenantID, "group_id", g.ID, "error", err)
		}
	}
	return nil
}

func (s *RefreshScheduler) tickGroup(
	ctx context.Context, g model.MailGroup, now time.Time, busy map[string]bool,
) error {
	if busy[g.TenantID] {
		return nil
	}
	active, err := s.store.CountActiveJobsByType(ctx, g.TenantID, model.JobTypeTokenRefresh)
	if err != nil {
		return err
	}
	if active > 0 {
		busy[g.TenantID] = true
		// 这里**有意不推进** next_refresh_at。
		//
		// 推进的话，一个用户给五个分组设了同样的间隔时，只有排在最前面的那个会
		// 真的被刷：它抢到任务之后，其余四个每一轮都恰好撞上「忙」，然后被推到
		// 下一个周期，周而复始。用户看到的是「有几个分组从来没被定时刷过」。
		// 不推进则它们只是排队，前一个任务跑完的下一分钟就轮到下一个。
		return nil
	}

	// 基准是 now，不是 g.NextRefreshAt。
	//
	// 按旧值累加的话，服务停机三天再启动时，一个 6 小时间隔的分组会被判定为
	// 欠了十二个周期，然后连着补跑十二轮——那不是「补上进度」，那是把服务商
	// 直接打到风控。用 now 则只补跑当前这一次，正是用户要的语义。
	next := now.Add(time.Duration(g.RefreshIntervalMinutes) * time.Minute)
	claimed, err := s.store.ClaimGroupRefresh(ctx, g.ID, now, next)
	if err != nil || !claimed {
		return err
	}

	j, err := s.refresh.submitScheduled(ctx, g.TenantID, g.ID)
	if errors.Is(err, ErrNoRefreshableAccounts) {
		// 分组里一个能刷的账号都没有（比如整组都是 IMAP 密码账号）。这不是故障，
		// 也不该每个周期在日志里喊一次；周期已经推进，下次照常再看。
		return nil
	}
	if err != nil {
		return err
	}
	busy[g.TenantID] = true
	slog.Info("已提交定时刷新任务", "job_id", j.ID, "tenant_id", g.TenantID,
		"group_id", g.ID, "count", j.TotalCount, "next_refresh_at", next)
	return nil
}
