package repo

import (
	"context"
	"time"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// DeleteRefreshLogsBefore 删除 cutoff 之前的刷新日志。
//
// 与任务清理同一个理由：定时刷新每个账号每轮写一条，这张表的增速从此跟着
// 账号数走。GetRefreshStats 的四个数字读的是 mail_accounts 不受影响，
// 但「最近 7 天失败原因」那条聚合会随历史堆积一起变慢。
func (s *Store) DeleteRefreshLogsBefore(ctx context.Context, cutoff time.Time) error {
	if s.driver == "sqlite" {
		return s.sqlite.DeleteRefreshLogsBefore(ctx, cutoff.UTC())
	}
	return s.postgres.DeleteRefreshLogsBefore(ctx, cutoff.UTC())
}

// CreateRefreshLog 记一次刷新结果。
func (s *Store) CreateRefreshLog(ctx context.Context, l model.RefreshLog) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreateRefreshLog(ctx, sqlitedb.CreateRefreshLogParams{
			ID: l.ID, TenantID: l.TenantID, AccountID: nullStr(l.AccountID),
			AccountEmail: l.AccountEmail, JobID: nullStr(l.JobID),
			RefreshType: l.RefreshType, Status: l.Status,
			ErrorKind: l.ErrorKind, ErrorMessage: l.ErrorMessage,
		})
	} else {
		err = s.postgres.CreateRefreshLog(ctx, postgresdb.CreateRefreshLogParams{
			ID: l.ID, TenantID: l.TenantID, AccountID: nullStr(l.AccountID),
			AccountEmail: l.AccountEmail, JobID: nullStr(l.JobID),
			RefreshType: l.RefreshType, Status: l.Status,
			ErrorKind: l.ErrorKind, ErrorMessage: l.ErrorMessage,
		})
	}
	return normalize(err)
}

// ListRefreshLogs 按条件分页取刷新日志。
func (s *Store) ListRefreshLogs(ctx context.Context, tenantID string, f model.RefreshLogFilter) ([]model.RefreshLog, int, error) {
	f.Normalize()
	out := []model.RefreshLog{}

	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListRefreshLogsPage(ctx, sqlitedb.ListRefreshLogsPageParams{
			TenantID: tenantID, Status: anyStr(f.Status),
			AccountID: anyStr(f.AccountID), JobID: anyStr(f.JobID),
			RowLimit: int64(f.Limit), RowOffset: int64(f.Offset()),
		})
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			out = append(out, mapSRefreshLog(r))
		}
		total, err := s.sqlite.CountRefreshLogs(ctx, sqlitedb.CountRefreshLogsParams{
			TenantID: tenantID, Status: anyStr(f.Status),
			AccountID: anyStr(f.AccountID), JobID: anyStr(f.JobID),
		})
		return out, int(total), err
	}

	rows, err := s.postgres.ListRefreshLogsPage(ctx, postgresdb.ListRefreshLogsPageParams{
		TenantID: tenantID, Status: nullStr(f.Status),
		AccountID: nullStr(f.AccountID), JobID: nullStr(f.JobID),
		RowLimit: int32(f.Limit), RowOffset: int32(f.Offset()),
	})
	if err != nil {
		return nil, 0, err
	}
	for _, r := range rows {
		out = append(out, mapPRefreshLog(r))
	}
	total, err := s.postgres.CountRefreshLogs(ctx, postgresdb.CountRefreshLogsParams{
		TenantID: tenantID, Status: nullStr(f.Status),
		AccountID: nullStr(f.AccountID), JobID: nullStr(f.JobID),
	})
	return out, int(total), err
}

// GetRefreshStats 取「现在有多少账号是好的」。
// 这四个数来自 mail_accounts 的 last_refresh_status，而不是日志表的历史计数。
func (s *Store) GetRefreshStats(ctx context.Context, tenantID string) (*model.RefreshStats, error) {
	stats := &model.RefreshStats{ByErrorKind: map[string]int{}}
	if s.driver == "sqlite" {
		r, err := s.sqlite.GetRefreshStats(ctx, tenantID)
		if err != nil {
			return nil, normalize(err)
		}
		stats.Total = int(r.Total)
		stats.Success, stats.Failed = int(asInt64(r.Success)), int(asInt64(r.Failed))
		stats.Never = int(asInt64(r.NeverRefreshed))
		return stats, nil
	}
	r, err := s.postgres.GetRefreshStats(ctx, tenantID)
	if err != nil {
		return nil, normalize(err)
	}
	stats.Total = int(r.Total)
	stats.Success, stats.Failed = int(asInt64(r.Success)), int(asInt64(r.Failed))
	stats.Never = int(asInt64(r.NeverRefreshed))
	return stats, nil
}

// GroupRefreshFailures 统计 since 之后失败原因的分布。
// 带时间窗是有意的：不加窗的话这条聚合会随历史增长慢慢退化成全表扫描。
func (s *Store) GroupRefreshFailures(ctx context.Context, tenantID string, since time.Time) (map[string]int, error) {
	out := map[string]int{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.GroupRefreshFailuresByKind(ctx, sqlitedb.GroupRefreshFailuresByKindParams{
			TenantID: tenantID, CreatedAt: since,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.ErrorKind] = int(r.Count)
		}
		return out, nil
	}
	rows, err := s.postgres.GroupRefreshFailuresByKind(ctx, postgresdb.GroupRefreshFailuresByKindParams{
		TenantID: tenantID, CreatedAt: since,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.ErrorKind] = int(r.Count)
	}
	return out, nil
}

func mapSRefreshLog(r sqlitedb.MailRefreshLog) model.RefreshLog {
	return model.RefreshLog{
		ID: r.ID, TenantID: r.TenantID, AccountID: r.AccountID.String,
		AccountEmail: r.AccountEmail, JobID: r.JobID.String, RefreshType: r.RefreshType,
		Status: r.Status, ErrorKind: r.ErrorKind, ErrorMessage: r.ErrorMessage,
		CreatedAt: r.CreatedAt,
	}
}

func mapPRefreshLog(r postgresdb.MailRefreshLog) model.RefreshLog {
	return model.RefreshLog{
		ID: r.ID, TenantID: r.TenantID, AccountID: r.AccountID.String,
		AccountEmail: r.AccountEmail, JobID: r.JobID.String, RefreshType: r.RefreshType,
		Status: r.Status, ErrorKind: r.ErrorKind, ErrorMessage: r.ErrorMessage,
		CreatedAt: r.CreatedAt,
	}
}
