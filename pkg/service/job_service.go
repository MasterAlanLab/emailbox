package service

import (
	"context"

	"emailbox/pkg/job"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
)

// JobService 是任务的查询侧：列表、详情、逐项结果、事件回放与停止。
//
// 每一个方法都带 tenantID，且底下的 SQL 都是 WHERE tenant_id = ?：
// 管理员走的是同一批方法，只是 tenantID 来自 URL 而不是 session。
type JobService struct {
	store   *repo.Store
	manager *job.Manager
}

func NewJobService(store *repo.Store, manager *job.Manager) *JobService {
	return &JobService{store: store, manager: manager}
}

func (s *JobService) List(ctx context.Context, tenantID string, f model.JobFilter) ([]model.Job, int, error) {
	return s.store.ListJobs(ctx, tenantID, f)
}

func (s *JobService) Get(ctx context.Context, tenantID, jobID string) (*model.Job, error) {
	return s.store.GetJob(ctx, tenantID, jobID)
}

func (s *JobService) Items(ctx context.Context, tenantID, jobID string, f model.JobItemFilter) ([]model.JobItem, int, error) {
	// 先确认这个任务属于该租户，再按 job_id 查明细。
	// job_items 上没有 tenant_id 列，少了这一步就等于「知道 job_id 就能看别人的明细」。
	if _, err := s.store.GetJob(ctx, tenantID, jobID); err != nil {
		return nil, 0, err
	}
	return s.store.ListJobItems(ctx, jobID, f)
}

// Events 回放 afterSeq 之后的事件，供 SSE 的首帧补齐与断线重连共用。
func (s *JobService) Events(ctx context.Context, tenantID, jobID string, afterSeq int64, limit int) ([]model.JobEvent, error) {
	if _, err := s.store.GetJob(ctx, tenantID, jobID); err != nil {
		return nil, err
	}
	return s.store.ListJobEventsAfter(ctx, jobID, afterSeq, limit)
}

// Stop 请求停止任务。真正的收尾由 worker 在下一个账号之前完成。
func (s *JobService) Stop(ctx context.Context, tenantID, jobID string) error {
	return s.manager.Stop(ctx, tenantID, jobID)
}

// Subscribe 订阅任务的变更信号，SSE 用它决定什么时候去读新事件。
func (s *JobService) Subscribe(jobID string) (<-chan struct{}, func()) {
	return s.manager.Broker().Subscribe(jobID)
}
