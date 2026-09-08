package server

import (
	"context"
	"log/slog"
	"time"

	"emailbox/configs"
	"emailbox/pkg/repo"
)

// 任务与刷新日志的保留期。
//
// 写成常量而不是环境变量：这两个值不需要按部署调整，而多一个环境变量就多一处
// 要在文档、.env.example、Docker 说明之间保持同步的东西。真要改就改这里。
//
// 30 天的依据是「还能用来排查」——令牌刷新的排查窗口是几天量级（哪批账号什么
// 时候开始失败），一个月已经远超；再长就只是在给 SQLite 单文件加体重。
const (
	jobRetention        = 30 * 24 * time.Hour
	refreshLogRetention = 30 * 24 * time.Hour
)

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

// purgeJobs 定期清理已结束的任务。job_items 与 job_events 随外键级联删除。
//
// 手动刷新是低频的，这张表一直没有清理也没出过问题。定时刷新把它变成了
// 「账号数 x 每天轮次」的稳定增量——5000 个账号每天四轮就是每天两万行 job_items。
//
// 出错只记日志不退出：清理是旁路，下个周期会重来，而让协程悄悄退出会变成
// 「表一直在涨但没有任何人知道」。下面 purgeRefreshLogs 同理。
func purgeJobs(ctx context.Context, store *repo.Store) {
	const interval = 6 * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cutoff := time.Now().Add(-jobRetention)
		if err := store.DeleteFinishedJobsBefore(ctx, cutoff); err != nil && ctx.Err() == nil {
			slog.Error("清理过期任务失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// purgeRefreshLogs 定期清理刷新日志。增长原因同 purgeJobs：每个账号每轮一条。
func purgeRefreshLogs(ctx context.Context, store *repo.Store) {
	const interval = 6 * time.Hour
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		cutoff := time.Now().Add(-refreshLogRetention)
		if err := store.DeleteRefreshLogsBefore(ctx, cutoff); err != nil && ctx.Err() == nil {
			slog.Error("清理过期刷新日志失败", "error", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
