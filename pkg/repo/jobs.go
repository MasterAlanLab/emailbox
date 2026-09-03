package repo

import (
	"context"
	"database/sql"
	"time"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// CreateJob 写入任务本体。
func (s *Store) CreateJob(ctx context.Context, j model.Job) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreateJob(ctx, sqlitedb.CreateJobParams{
			ID: j.ID, TenantID: j.TenantID, Type: j.Type, Trigger: j.Trigger,
			Status: j.Status, CreatedBy: nullStr(j.CreatedBy),
			TotalCount: int64(j.TotalCount), Params: j.Params,
		})
	} else {
		err = s.postgres.CreateJob(ctx, postgresdb.CreateJobParams{
			ID: j.ID, TenantID: j.TenantID, Type: j.Type, Trigger: j.Trigger,
			Status: j.Status, CreatedBy: nullStr(j.CreatedBy),
			TotalCount: int32(j.TotalCount), Params: j.Params,
		})
	}
	return normalize(err)
}

// GetJob 按租户取任务。跨租户取会落到 ErrNotFound，而不是返回别人的任务。
func (s *Store) GetJob(ctx context.Context, tenantID, jobID string) (*model.Job, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetJob(ctx, sqlitedb.GetJobParams{TenantID: tenantID, ID: jobID})
		if err != nil {
			return nil, normalize(err)
		}
		j := mapSJob(v)
		return &j, nil
	}
	v, err := s.postgres.GetJob(ctx, postgresdb.GetJobParams{TenantID: tenantID, ID: jobID})
	if err != nil {
		return nil, normalize(err)
	}
	j := mapPJob(v)
	return &j, nil
}

// ListJobs 按条件分页取任务，同时返回总条数。
func (s *Store) ListJobs(ctx context.Context, tenantID string, f model.JobFilter) ([]model.Job, int, error) {
	f.Normalize()
	out := []model.Job{}

	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListJobsPage(ctx, sqlitedb.ListJobsPageParams{
			TenantID: tenantID, Type: anyStr(f.Type), Status: anyStr(f.Status),
			RowLimit: int64(f.Limit), RowOffset: int64(f.Offset()),
		})
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			out = append(out, mapSJob(r))
		}
		total, err := s.sqlite.CountJobs(ctx, sqlitedb.CountJobsParams{
			TenantID: tenantID, Type: anyStr(f.Type), Status: anyStr(f.Status),
		})
		return out, int(total), err
	}

	rows, err := s.postgres.ListJobsPage(ctx, postgresdb.ListJobsPageParams{
		TenantID: tenantID, Type: nullStr(f.Type), Status: nullStr(f.Status),
		RowLimit: int32(f.Limit), RowOffset: int32(f.Offset()),
	})
	if err != nil {
		return nil, 0, err
	}
	for _, r := range rows {
		out = append(out, mapPJob(r))
	}
	total, err := s.postgres.CountJobs(ctx, postgresdb.CountJobsParams{
		TenantID: tenantID, Type: nullStr(f.Type), Status: nullStr(f.Status),
	})
	return out, int(total), err
}

// StartJob 把 pending 置为 running。返回是否真的由本次调用启动——
// 用它做「只有一个 worker 能启动这个任务」的判定。
func (s *Store) StartJob(ctx context.Context, jobID string) (bool, error) {
	var (
		n   int64
		err error
	)
	if s.driver == "sqlite" {
		n, err = s.sqlite.StartJob(ctx, jobID)
	} else {
		n, err = s.postgres.StartJob(ctx, jobID)
	}
	return n > 0, normalize(err)
}

// TouchJobHeartbeat 更新心跳。进程被强杀后没人再更新它，
// 下次启动时僵尸任务就是靠这个时间戳认出来的。
func (s *Store) TouchJobHeartbeat(ctx context.Context, jobID string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.TouchJobHeartbeat(ctx, jobID))
	}
	return normalize(s.postgres.TouchJobHeartbeat(ctx, jobID))
}

// BumpJobCounts 相对累加成功/失败计数。
//
// 必须是相对 UPDATE，不能「读出来加一再写回去」：N 个 worker 同时完成 item 时，
// 读改写会丢增量，最终计数比实际少——而且少多少取决于时序，测都不好测。
func (s *Store) BumpJobCounts(ctx context.Context, jobID string, success, failed int) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.BumpJobCounts(ctx, sqlitedb.BumpJobCountsParams{
			SuccessCount: int64(success), FailedCount: int64(failed), ID: jobID,
		}))
	}
	return normalize(s.postgres.BumpJobCounts(ctx, postgresdb.BumpJobCountsParams{
		SuccessCount: int32(success), FailedCount: int32(failed), ID: jobID,
	}))
}

// FinishJob 写入终态。
func (s *Store) FinishJob(ctx context.Context, jobID, status, summary string) error {
	if s.driver == "sqlite" {
		return rowsAffected(s.sqlite.FinishJob(ctx, sqlitedb.FinishJobParams{
			Status: status, ErrorSummary: summary, ID: jobID,
		}))
	}
	return rowsAffected(s.postgres.FinishJob(ctx, postgresdb.FinishJobParams{
		Status: status, ErrorSummary: summary, ID: jobID,
	}))
}

// RequestJobStop 请求停止。已经结束的任务会落到 ErrNotFound。
func (s *Store) RequestJobStop(ctx context.Context, tenantID, jobID string) error {
	if s.driver == "sqlite" {
		return rowsAffected(s.sqlite.RequestJobStop(ctx, sqlitedb.RequestJobStopParams{
			TenantID: tenantID, ID: jobID,
		}))
	}
	return rowsAffected(s.postgres.RequestJobStop(ctx, postgresdb.RequestJobStopParams{
		TenantID: tenantID, ID: jobID,
	}))
}

// CountActiveJobsByType 数一个租户下还没终结的某类任务。
//
// 调度器靠它避免在同一个租户里叠任务：Manager 每个任务各起 Workers 个 worker，
// 两个刷新任务并行就是双倍并发打同一个服务商，而 JOB_ACCOUNT_DELAY_MS
// 只在单个 worker 内部起作用，拦不住这种叠加。
func (s *Store) CountActiveJobsByType(ctx context.Context, tenantID, jobType string) (int, error) {
	if s.driver == "sqlite" {
		n, err := s.sqlite.CountActiveJobsByType(ctx, sqlitedb.CountActiveJobsByTypeParams{
			TenantID: tenantID, Type: jobType,
		})
		return int(n), err
	}
	n, err := s.postgres.CountActiveJobsByType(ctx, postgresdb.CountActiveJobsByTypeParams{
		TenantID: tenantID, Type: jobType,
	})
	return int(n), err
}

// DeleteFinishedJobsBefore 删除 cutoff 之前结束的任务，items 与 events 随外键级联。
//
// 手动刷新是低频的，这张表一直没有清理也没出过问题；定时刷新会把它变成
// 「账号数 x 每天次数」的稳定增量，不清理迟早拖垮 SQLite 单文件。
func (s *Store) DeleteFinishedJobsBefore(ctx context.Context, cutoff time.Time) error {
	at := utcNullTime(cutoff)
	if s.driver == "sqlite" {
		return s.sqlite.DeleteFinishedJobsBefore(ctx, at)
	}
	return s.postgres.DeleteFinishedJobsBefore(ctx, at)
}

// GetJobStatus 只取状态。worker 在每个 item 之前问一次，用来响应停止请求。
func (s *Store) GetJobStatus(ctx context.Context, jobID string) (string, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetJobStatus(ctx, jobID)
		return v, normalize(err)
	}
	v, err := s.postgres.GetJobStatus(ctx, jobID)
	return v, normalize(err)
}

// ListStaleJobs 找出心跳早于 cutoff 且仍未终结的任务，即崩溃遗留的僵尸。
func (s *Store) ListStaleJobs(ctx context.Context, cutoff time.Time) ([]model.Job, error) {
	out := []model.Job{}
	at := sql.NullTime{Time: cutoff, Valid: true}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListStaleJobs(ctx, at)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, mapSJob(r))
		}
		return out, nil
	}
	rows, err := s.postgres.ListStaleJobs(ctx, at)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, mapPJob(r))
	}
	return out, nil
}

// MarkJobInterrupted 把僵尸任务标为 interrupted。
func (s *Store) MarkJobInterrupted(ctx context.Context, jobID, summary string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.MarkJobInterrupted(ctx, sqlitedb.MarkJobInterruptedParams{
			ID: jobID, ErrorSummary: summary,
		}))
	}
	return normalize(s.postgres.MarkJobInterrupted(ctx, postgresdb.MarkJobInterruptedParams{
		ID: jobID, ErrorSummary: summary,
	}))
}

// CreateJobItems 批量写入逐项。放在一个事务里：5000 条逐条提交在 SQLite 上
// 是 5000 次 fsync，提交一个任务要等十几秒。
func (s *Store) CreateJobItems(ctx context.Context, items []model.JobItem) error {
	return s.WithTx(ctx, func(tx *Store) error {
		for _, item := range items {
			var err error
			if tx.driver == "sqlite" {
				err = tx.sqlite.CreateJobItem(ctx, sqlitedb.CreateJobItemParams{
					ID: item.ID, JobID: item.JobID, AccountID: nullStr(item.AccountID),
					Email: item.Email, Position: int64(item.Position),
				})
			} else {
				err = tx.postgres.CreateJobItem(ctx, postgresdb.CreateJobItemParams{
					ID: item.ID, JobID: item.JobID, AccountID: nullStr(item.AccountID),
					Email: item.Email, Position: int32(item.Position),
				})
			}
			if err != nil {
				return normalize(err)
			}
		}
		return nil
	})
}

// ListPendingJobItems 取还没处理的逐项，按提交顺序。
func (s *Store) ListPendingJobItems(ctx context.Context, jobID string) ([]model.JobItem, error) {
	out := []model.JobItem{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListPendingJobItems(ctx, jobID)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, mapSJobItem(r))
		}
		return out, nil
	}
	rows, err := s.postgres.ListPendingJobItems(ctx, jobID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, mapPJobItem(r))
	}
	return out, nil
}

// ListJobItems 按条件分页取逐项结果。
func (s *Store) ListJobItems(ctx context.Context, jobID string, f model.JobItemFilter) ([]model.JobItem, int, error) {
	f.Normalize()
	out := []model.JobItem{}

	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListJobItemsPage(ctx, sqlitedb.ListJobItemsPageParams{
			JobID: jobID, Status: anyStr(f.Status),
			RowLimit: int64(f.Limit), RowOffset: int64(f.Offset()),
		})
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			out = append(out, mapSJobItem(r))
		}
		total, err := s.sqlite.CountJobItems(ctx, sqlitedb.CountJobItemsParams{
			JobID: jobID, Status: anyStr(f.Status),
		})
		return out, int(total), err
	}

	rows, err := s.postgres.ListJobItemsPage(ctx, postgresdb.ListJobItemsPageParams{
		JobID: jobID, Status: nullStr(f.Status),
		RowLimit: int32(f.Limit), RowOffset: int32(f.Offset()),
	})
	if err != nil {
		return nil, 0, err
	}
	for _, r := range rows {
		out = append(out, mapPJobItem(r))
	}
	total, err := s.postgres.CountJobItems(ctx, postgresdb.CountJobItemsParams{
		JobID: jobID, Status: nullStr(f.Status),
	})
	return out, int(total), err
}

// StartJobItem 标记某一项开始处理。
func (s *Store) StartJobItem(ctx context.Context, itemID string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.StartJobItem(ctx, itemID))
	}
	return normalize(s.postgres.StartJobItem(ctx, itemID))
}

// FinishJobItem 写入某一项的结果。
func (s *Store) FinishJobItem(ctx context.Context, itemID, status, errorKind, message string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.FinishJobItem(ctx, sqlitedb.FinishJobItemParams{
			Status: status, ErrorKind: errorKind, Error: message, ID: itemID,
		}))
	}
	return normalize(s.postgres.FinishJobItem(ctx, postgresdb.FinishJobItemParams{
		Status: status, ErrorKind: errorKind, Error: message, ID: itemID,
	}))
}

// CreateJobEvent 追加一条事件。
func (s *Store) CreateJobEvent(ctx context.Context, e model.JobEvent) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.CreateJobEvent(ctx, sqlitedb.CreateJobEventParams{
			ID: e.ID, JobID: e.JobID, Seq: e.Seq, Kind: e.Kind, Payload: e.Payload,
		}))
	}
	return normalize(s.postgres.CreateJobEvent(ctx, postgresdb.CreateJobEventParams{
		ID: e.ID, JobID: e.JobID, Seq: int32(e.Seq), Kind: e.Kind, Payload: e.Payload,
	}))
}

// ListJobEventsAfter 回放 seq 之后的事件，供 SSE 断线重连。
func (s *Store) ListJobEventsAfter(ctx context.Context, jobID string, afterSeq int64, limit int) ([]model.JobEvent, error) {
	out := []model.JobEvent{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListJobEventsAfter(ctx, sqlitedb.ListJobEventsAfterParams{
			JobID: jobID, Seq: afterSeq, Limit: int64(limit),
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, model.JobEvent{
				ID: r.ID, JobID: r.JobID, Seq: r.Seq, Kind: r.Kind,
				Payload: r.Payload, CreatedAt: r.CreatedAt,
			})
		}
		return out, nil
	}
	rows, err := s.postgres.ListJobEventsAfter(ctx, postgresdb.ListJobEventsAfterParams{
		JobID: jobID, Seq: int32(afterSeq), Limit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, model.JobEvent{
			ID: r.ID, JobID: r.JobID, Seq: int64(r.Seq), Kind: r.Kind,
			Payload: r.Payload, CreatedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// MaxJobEventSeq 返回当前最大序号，进程重启后由它接着往下发号。
func (s *Store) MaxJobEventSeq(ctx context.Context, jobID string) (int64, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.MaxJobEventSeq(ctx, jobID)
		return asInt64(v), normalize(err)
	}
	v, err := s.postgres.MaxJobEventSeq(ctx, jobID)
	return asInt64(v), normalize(err)
}

// DeleteJobEventsBefore 清理过期事件。事件量与账号数成正比，不清会一直涨。
func (s *Store) DeleteJobEventsBefore(ctx context.Context, cutoff time.Time) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.DeleteJobEventsBefore(ctx, cutoff))
	}
	return normalize(s.postgres.DeleteJobEventsBefore(ctx, cutoff))
}

func mapSJob(j sqlitedb.Job) model.Job {
	return model.Job{
		ID: j.ID, TenantID: j.TenantID, Type: j.Type, Trigger: j.Trigger, Status: j.Status,
		CreatedBy: j.CreatedBy.String, TotalCount: int(j.TotalCount),
		SuccessCount: int(j.SuccessCount), FailedCount: int(j.FailedCount),
		Params: j.Params, ErrorSummary: j.ErrorSummary,
		StartedAt: timePtr(j.StartedAt), FinishedAt: timePtr(j.FinishedAt),
		HeartbeatAt: timePtr(j.HeartbeatAt), CreatedAt: j.CreatedAt,
	}
}

func mapPJob(j postgresdb.Job) model.Job {
	return model.Job{
		ID: j.ID, TenantID: j.TenantID, Type: j.Type, Trigger: j.Trigger, Status: j.Status,
		CreatedBy: j.CreatedBy.String, TotalCount: int(j.TotalCount),
		SuccessCount: int(j.SuccessCount), FailedCount: int(j.FailedCount),
		Params: j.Params, ErrorSummary: j.ErrorSummary,
		StartedAt: timePtr(j.StartedAt), FinishedAt: timePtr(j.FinishedAt),
		HeartbeatAt: timePtr(j.HeartbeatAt), CreatedAt: j.CreatedAt,
	}
}

func mapSJobItem(i sqlitedb.JobItem) model.JobItem {
	return model.JobItem{
		ID: i.ID, JobID: i.JobID, AccountID: i.AccountID.String, Email: i.Email,
		Position: int(i.Position), Status: i.Status, ErrorKind: i.ErrorKind, Error: i.Error,
		StartedAt: timePtr(i.StartedAt), FinishedAt: timePtr(i.FinishedAt),
	}
}

func mapPJobItem(i postgresdb.JobItem) model.JobItem {
	return model.JobItem{
		ID: i.ID, JobID: i.JobID, AccountID: i.AccountID.String, Email: i.Email,
		Position: int(i.Position), Status: i.Status, ErrorKind: i.ErrorKind, Error: i.Error,
		StartedAt: timePtr(i.StartedAt), FinishedAt: timePtr(i.FinishedAt),
	}
}
