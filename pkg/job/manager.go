package job

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

// Result 是一个 item 的处理结果。
type Result struct {
	// Status 取 model.JobItemSuccess / JobItemFailed / JobItemSkipped。
	Status    string
	ErrorKind string
	Message   string
}

// Runner 执行单个 item。由业务侧实现（RefreshService），
// job 包因此完全不认识邮件协议，只管调度、计数、落库与事件。
type Runner interface {
	// Type 返回它能处理的任务类型，与 jobs.type 对应。
	Type() string
	Run(ctx context.Context, job model.Job, item model.JobItem) Result
}

// Config 是调度参数。
type Config struct {
	// Workers 是每个任务内部的并发数。
	Workers int
	// AccountDelay 是同一个 worker 处理两个账号之间的间隔，用来避开服务商风控
	// （对应 outlookEmail 的 refresh_delay_seconds）。
	AccountDelay time.Duration
	// Heartbeat 是心跳写库间隔。
	Heartbeat time.Duration
	// StaleAfter 超过这个时长没有心跳的 running 任务视为僵尸。
	// 必须明显大于 Heartbeat，否则正常任务会被自己的启动扫描误杀。
	StaleAfter time.Duration
	// ProgressEvery 控制 progress 事件的最小间隔，避免 5000 个账号写出
	// 5000 条 progress（item 事件本来就每个一条，够用了）。
	ProgressEvery time.Duration
}

func (c *Config) applyDefaults() {
	if c.Workers <= 0 {
		c.Workers = 8
	}
	if c.Heartbeat <= 0 {
		c.Heartbeat = 5 * time.Second
	}
	if c.StaleAfter <= 0 {
		c.StaleAfter = 2 * time.Minute
	}
	if c.ProgressEvery < 0 {
		c.ProgressEvery = 0
	}
	if c.ProgressEvery == 0 {
		c.ProgressEvery = time.Second
	}
}

// Manager 调度任务。单实例设计（见 02 文档 §4.3），SQLite 部署不支持多实例。
type Manager struct {
	store  *repo.Store
	cfg    Config
	broker *Broker

	mu      sync.Mutex
	runners map[string]Runner
	// running 保存在跑的任务的取消函数，供优雅关机使用。
	running map[string]context.CancelFunc
	wg      sync.WaitGroup

	// seq 是各任务的事件序号发号器。进程重启后从库里的最大值接着发。
	seqMu sync.Mutex
	seq   map[string]*atomic.Int64
}

func New(store *repo.Store, cfg Config) *Manager {
	cfg.applyDefaults()
	return &Manager{
		store:   store,
		cfg:     cfg,
		broker:  NewBroker(),
		runners: make(map[string]Runner),
		running: make(map[string]context.CancelFunc),
		seq:     make(map[string]*atomic.Int64),
	}
}

func (m *Manager) Broker() *Broker { return m.broker }

// Register 注册某种任务类型的执行器。
func (m *Manager) Register(r Runner) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runners[r.Type()] = r
}

// ErrNoRunner 表示这种任务类型没有注册执行器。
var ErrNoRunner = errors.New("没有可处理该任务类型的执行器")

// Submit 落库并异步开跑。
//
// 先写库再启动：任何一步失败都不会留下「内存里在跑、库里没有」的任务。
// 返回时任务已经可查（status=pending），前端可以立刻订阅它的事件流。
func (m *Manager) Submit(ctx context.Context, job model.Job, items []model.JobItem) error {
	m.mu.Lock()
	_, ok := m.runners[job.Type]
	m.mu.Unlock()
	if !ok {
		return ErrNoRunner
	}

	if err := m.store.CreateJob(ctx, job); err != nil {
		return err
	}
	if len(items) > 0 {
		if err := m.store.CreateJobItems(ctx, items); err != nil {
			return err
		}
	}

	m.spawn(job)
	return nil
}

// spawn 在后台跑一个任务。
//
// 用 context.WithoutCancel 派生：提交这个任务的那个 HTTP 请求马上就结束了，
// 跟着它的 context 走会让任务在几毫秒后被取消。取消能力由 Shutdown 单独提供。
func (m *Manager) spawn(job model.Job) {
	ctx, cancel := context.WithCancel(context.WithoutCancel(context.Background()))

	m.mu.Lock()
	m.running[job.ID] = cancel
	m.mu.Unlock()

	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		defer func() {
			m.mu.Lock()
			delete(m.running, job.ID)
			m.mu.Unlock()
			cancel()
		}()
		m.run(ctx, job)
	}()
}

// Stop 请求停止。只是把状态改成 stopping，真正的收尾由 worker 在下一个
// item 之前完成——正在打上游的那一次请求不打断，否则会留下一个
// 「已经刷新成功但没写回」的账号，下次刷新必然失败。
func (m *Manager) Stop(ctx context.Context, tenantID, jobID string) error {
	if err := m.store.RequestJobStop(ctx, tenantID, jobID); err != nil {
		return err
	}
	m.broker.Notify(jobID)
	return nil
}

// ReapStale 把心跳超时且仍未终结的任务标为 interrupted。启动时调用一次。
//
// 没有这一步的话，一次强杀会在库里留下永远 running 的任务：前端的进度条
// 会一直转下去，而且用户没法重新提交（界面会认为还有任务在跑）。
func (m *Manager) ReapStale(ctx context.Context) (int, error) {
	jobs, err := m.store.ListStaleJobs(ctx, time.Now().Add(-m.cfg.StaleAfter))
	if err != nil {
		return 0, err
	}
	for _, j := range jobs {
		if err := m.store.MarkJobInterrupted(ctx, j.ID, "服务重启，任务中断"); err != nil {
			return 0, err
		}
		slog.Warn("发现中断的任务并已标记", "job_id", j.ID, "tenant_id", j.TenantID,
			"success", j.SuccessCount, "failed", j.FailedCount, "total", j.TotalCount)
	}
	return len(jobs), nil
}

// Shutdown 取消所有在跑的任务并等它们退出。
// 未处理完的 item 留在 pending，任务会被下次启动的 ReapStale 标为 interrupted。
func (m *Manager) Shutdown(ctx context.Context) {
	m.mu.Lock()
	for _, cancel := range m.running {
		cancel()
	}
	m.mu.Unlock()

	done := make(chan struct{})
	go func() {
		m.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
		slog.Warn("等待任务退出超时，仍在跑的任务会在下次启动时标为 interrupted")
	}
}

// nextSeq 取某任务的下一个事件序号。
func (m *Manager) nextSeq(ctx context.Context, jobID string) int64 {
	m.seqMu.Lock()
	counter, ok := m.seq[jobID]
	if !ok {
		counter = &atomic.Int64{}
		// 进程重启后接着库里的最大值发号，否则会和已有事件撞 UNIQUE(job_id, seq)。
		if latest, err := m.store.MaxJobEventSeq(ctx, jobID); err == nil {
			counter.Store(latest)
		}
		m.seq[jobID] = counter
	}
	m.seqMu.Unlock()
	return counter.Add(1)
}

// emit 落一条事件并唤醒订阅者。
//
// 事件写失败只记日志：它是观测通道，不该让业务跟着失败。
// 真正的进度以 jobs 表的计数为准，前端断线重连时看到的也是那个。
func (m *Manager) emit(ctx context.Context, jobID, kind string, payload any) {
	encoded := "{}"
	if payload != nil {
		if b, err := json.Marshal(payload); err == nil {
			encoded = string(b)
		}
	}
	event := model.JobEvent{
		ID: uuid.NewString(), JobID: jobID, Seq: m.nextSeq(ctx, jobID),
		Kind: kind, Payload: encoded,
	}
	if err := m.store.CreateJobEvent(ctx, event); err != nil {
		slog.Warn("写任务事件失败", "job_id", jobID, "kind", kind, "error", err)
		return
	}
	m.broker.Notify(jobID)
}
