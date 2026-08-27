package job

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"emailbox/pkg/model"
)

// progressPayload 是 progress 事件的内容。
type progressPayload struct {
	Total   int    `json:"total"`
	Success int    `json:"success"`
	Failed  int    `json:"failed"`
	Done    int    `json:"done"`
	Current string `json:"current"`
}

// itemPayload 是 item 事件的内容。
type itemPayload struct {
	AccountID string `json:"account_id"`
	Email     string `json:"email"`
	Status    string `json:"status"`
	ErrorKind string `json:"error_kind"`
	Error     string `json:"error"`
}

// finishedPayload 是 finished 事件的内容。
type finishedPayload struct {
	Status       string `json:"status"`
	Total        int    `json:"total"`
	Success      int    `json:"success"`
	Failed       int    `json:"failed"`
	Skipped      int    `json:"skipped"`
	ErrorSummary string `json:"error_summary"`
}

// run 是一个任务的完整生命周期。
func (m *Manager) run(ctx context.Context, job model.Job) {
	m.mu.Lock()
	runner := m.runners[job.Type]
	m.mu.Unlock()
	if runner == nil {
		slog.Error("任务没有执行器", "job_id", job.ID, "type", job.Type)
		return
	}

	// StartJob 只会成功一次（WHERE status = 'pending'）。它同时是一道
	// 「同一个任务不会被跑两遍」的闸门。
	started, err := m.store.StartJob(ctx, job.ID)
	if err != nil {
		slog.Error("启动任务失败", "job_id", job.ID, "error", err)
		return
	}
	if !started {
		return
	}
	m.emit(ctx, job.ID, model.JobEventStarted, map[string]any{
		"total": job.TotalCount, "type": job.Type,
	})

	items, err := m.store.ListPendingJobItems(ctx, job.ID)
	if err != nil {
		m.failJob(ctx, job, "读取任务明细失败: "+err.Error())
		return
	}

	stopHeartbeat := m.startHeartbeat(ctx, job.ID)
	counts := m.dispatch(ctx, job, runner, items)
	stopHeartbeat()

	m.finish(ctx, job, counts)
}

// counters 汇总一个任务的处理结果。
type counters struct {
	success atomic.Int64
	failed  atomic.Int64
	skipped atomic.Int64
	// stopped 记录本次是不是因为收到停止请求而提前结束。
	stopped atomic.Bool
}

// dispatch 把 items 分给 worker 并等它们跑完。
func (m *Manager) dispatch(ctx context.Context, job model.Job, runner Runner, items []model.JobItem) *counters {
	c := &counters{}
	queue := make(chan model.JobItem)

	// progress 事件按时间节流。用互斥量护一个普通时间戳即可——
	// 这里争用极低（每个 item 一次），没必要为它设计无锁结构。
	var progressMu sync.Mutex
	lastProgress := time.Time{}

	var wg sync.WaitGroup
	for range m.cfg.Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for item := range queue {
				// 每个 item 之前检查一次停止请求。不打断正在进行的那一次上游调用：
				// 打断会留下「已经刷新成功但没写回」的账号，下次刷新必然失败。
				if m.shouldStop(ctx, job.ID) {
					c.stopped.Store(true)
					m.skip(ctx, job.ID, item, c)
					continue
				}
				if ctx.Err() != nil {
					// 进程要关了：剩下的留在 pending，由下次启动的 ReapStale 处理。
					return
				}

				m.runItem(ctx, job, runner, item, c)

				// item 事件本来就每个一条，再逐个写 progress 等于把 job_events 写成两倍大。
				progressMu.Lock()
				due := time.Since(lastProgress) >= m.cfg.ProgressEvery
				if due {
					lastProgress = time.Now()
				}
				progressMu.Unlock()
				if due {
					m.emitProgress(ctx, job, c, item.Email)
				}

				if m.cfg.AccountDelay > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(m.cfg.AccountDelay):
					}
				}
			}
		}()
	}

	for _, item := range items {
		select {
		case <-ctx.Done():
		case queue <- item:
			continue
		}
		break
	}
	close(queue)
	wg.Wait()

	// 收尾时补一条 progress，保证前端拿到的最后一个进度是准的
	// （最后几个 item 很可能被节流吃掉了）。
	m.emitProgress(ctx, job, c, "")
	return c
}

// runItem 处理一个账号。
func (m *Manager) runItem(ctx context.Context, job model.Job, runner Runner, item model.JobItem, c *counters) {
	if err := m.store.StartJobItem(ctx, item.ID); err != nil {
		slog.Warn("标记明细开始失败", "item_id", item.ID, "error", err)
	}

	result := runner.Run(ctx, job, item)
	if result.Status == "" {
		result.Status = model.JobItemFailed
		result.Message = "执行器没有返回结果"
	}

	if err := m.store.FinishJobItem(ctx, item.ID, result.Status, result.ErrorKind, result.Message); err != nil {
		slog.Warn("写入明细结果失败", "item_id", item.ID, "error", err)
	}

	// 计数用相对累加的 SQL，多个 worker 同时完成也不会丢增量。
	switch result.Status {
	case model.JobItemSuccess:
		c.success.Add(1)
		if err := m.store.BumpJobCounts(ctx, job.ID, 1, 0); err != nil {
			slog.Warn("累加成功计数失败", "job_id", job.ID, "error", err)
		}
	case model.JobItemSkipped:
		c.skipped.Add(1)
	default:
		c.failed.Add(1)
		if err := m.store.BumpJobCounts(ctx, job.ID, 0, 1); err != nil {
			slog.Warn("累加失败计数失败", "job_id", job.ID, "error", err)
		}
	}

	m.emit(ctx, job.ID, model.JobEventItem, itemPayload{
		AccountID: item.AccountID, Email: item.Email, Status: result.Status,
		ErrorKind: result.ErrorKind, Error: result.Message,
	})
}

// skip 把一个还没轮到就被停止的 item 标为 skipped。
func (m *Manager) skip(ctx context.Context, jobID string, item model.JobItem, c *counters) {
	c.skipped.Add(1)
	if err := m.store.FinishJobItem(ctx, item.ID, model.JobItemSkipped, "", "任务已停止"); err != nil {
		slog.Warn("标记明细跳过失败", "item_id", item.ID, "error", err)
	}
	m.emit(ctx, jobID, model.JobEventItem, itemPayload{
		AccountID: item.AccountID, Email: item.Email, Status: model.JobItemSkipped,
	})
}

// shouldStop 查库判断是否收到了停止请求。
// 每个 item 查一次，5000 个账号就是 5000 次单行主键查询——相对于每个账号
// 一次跨网络的令牌刷新（秒级），这点开销可以忽略。
func (m *Manager) shouldStop(ctx context.Context, jobID string) bool {
	status, err := m.store.GetJobStatus(ctx, jobID)
	if err != nil {
		return false
	}
	return status == model.JobStatusStopping
}

func (m *Manager) emitProgress(ctx context.Context, job model.Job, c *counters, current string) {
	success, failed, skipped := int(c.success.Load()), int(c.failed.Load()), int(c.skipped.Load())
	m.emit(ctx, job.ID, model.JobEventProgress, progressPayload{
		Total: job.TotalCount, Success: success, Failed: failed,
		Done: success + failed + skipped, Current: current,
	})
}

// startHeartbeat 起一个后台心跳，返回停止函数。
func (m *Manager) startHeartbeat(ctx context.Context, jobID string) func() {
	ticker := time.NewTicker(m.cfg.Heartbeat)
	done := make(chan struct{})

	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := m.store.TouchJobHeartbeat(ctx, jobID); err != nil {
					slog.Warn("更新任务心跳失败", "job_id", jobID, "error", err)
				}
			}
		}
	}()

	var once sync.Once
	return func() { once.Do(func() { close(done) }) }
}

// finish 计算终态并落库。
func (m *Manager) finish(ctx context.Context, job model.Job, c *counters) {
	success, failed, skipped := int(c.success.Load()), int(c.failed.Load()), int(c.skipped.Load())

	status := model.JobStatusSucceeded
	summary := ""
	switch {
	case c.stopped.Load():
		status = model.JobStatusStopped
		summary = fmt.Sprintf("任务被停止，已处理 %d 个，跳过 %d 个", success+failed, skipped)
	case failed > 0 && success == 0:
		status = model.JobStatusFailed
		summary = fmt.Sprintf("全部 %d 个账号处理失败", failed)
	case failed > 0:
		status = model.JobStatusPartial
		summary = fmt.Sprintf("%d 个成功，%d 个失败", success, failed)
	}

	if err := m.store.FinishJob(ctx, job.ID, status, summary); err != nil {
		slog.Error("写入任务终态失败", "job_id", job.ID, "error", err)
	}
	m.emit(ctx, job.ID, model.JobEventFinished, finishedPayload{
		Status: status, Total: job.TotalCount, Success: success,
		Failed: failed, Skipped: skipped, ErrorSummary: summary,
	})

	// 任务已终结，序号发号器可以丢了，否则长期运行的进程会越攒越多。
	m.seqMu.Lock()
	delete(m.seq, job.ID)
	m.seqMu.Unlock()
}

// failJob 用于任务还没开始逐项处理就失败的情况。
func (m *Manager) failJob(ctx context.Context, job model.Job, reason string) {
	if err := m.store.FinishJob(ctx, job.ID, model.JobStatusFailed, reason); err != nil {
		slog.Error("写入任务失败状态失败", "job_id", job.ID, "error", err)
	}
	m.emit(ctx, job.ID, model.JobEventError, map[string]any{"error": reason})
}
