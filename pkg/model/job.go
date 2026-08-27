package model

import "time"

// 任务类型。本期只有 token_refresh，P5 的转发轮询会加进来。
const (
	JobTypeTokenRefresh = "token_refresh"
)

// 任务触发方式。
const (
	JobTriggerManual    = "manual"
	JobTriggerScheduled = "scheduled"
	JobTriggerAPI       = "api"
)

// 任务状态。
//
// 终态有五个：succeeded / partial / failed / stopped / interrupted。
// interrupted 单列一个而不是并进 failed，是因为它说的是「进程没了」而不是
// 「这批账号刷不动」——前者要重启后重跑，后者要去看账号本身。
const (
	JobStatusPending     = "pending"
	JobStatusRunning     = "running"
	JobStatusStopping    = "stopping"
	JobStatusSucceeded   = "succeeded"
	JobStatusPartial     = "partial"
	JobStatusFailed      = "failed"
	JobStatusStopped     = "stopped"
	JobStatusInterrupted = "interrupted"
)

// IsTerminalJobStatus 判断任务是否已经结束。
func IsTerminalJobStatus(status string) bool {
	switch status {
	case JobStatusSucceeded, JobStatusPartial, JobStatusFailed,
		JobStatusStopped, JobStatusInterrupted:
		return true
	default:
		return false
	}
}

// 逐项状态。skipped 用于「任务被停止时还没轮到的那些」。
const (
	JobItemPending = "pending"
	JobItemRunning = "running"
	JobItemSuccess = "success"
	JobItemFailed  = "failed"
	JobItemSkipped = "skipped"
)

// SSE 事件类型。与 05 文档 §6 的事件流一致。
const (
	JobEventStarted  = "started"
	JobEventProgress = "progress"
	JobEventItem     = "item"
	JobEventFinished = "finished"
	JobEventError    = "error"
)

// Job 是一次批量任务的聚合状态。
type Job struct {
	ID           string     `json:"id"`
	TenantID     string     `json:"tenant_id"`
	Type         string     `json:"type"`
	Trigger      string     `json:"trigger"`
	Status       string     `json:"status"`
	CreatedBy    string     `json:"created_by"`
	TotalCount   int        `json:"total_count"`
	SuccessCount int        `json:"success_count"`
	FailedCount  int        `json:"failed_count"`
	Params       string     `json:"params"`
	ErrorSummary string     `json:"error_summary"`
	StartedAt    *time.Time `json:"started_at"`
	FinishedAt   *time.Time `json:"finished_at"`
	HeartbeatAt  *time.Time `json:"heartbeat_at"`
	CreatedAt    time.Time  `json:"created_at"`
}

// JobItem 是任务里的一个账号。
type JobItem struct {
	ID         string     `json:"id"`
	JobID      string     `json:"job_id"`
	AccountID  string     `json:"account_id"`
	Email      string     `json:"email"`
	Position   int        `json:"position"`
	Status     string     `json:"status"`
	ErrorKind  string     `json:"error_kind"`
	Error      string     `json:"error"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// JobEvent 是事件流里的一条。Seq 单调递增，SSE 的 Last-Event-ID 用它续看。
type JobEvent struct {
	ID        string    `json:"id"`
	JobID     string    `json:"job_id"`
	Seq       int64     `json:"seq"`
	Kind      string    `json:"kind"`
	Payload   string    `json:"payload"`
	CreatedAt time.Time `json:"created_at"`
}

// JobFilter 是任务列表的查询条件。
type JobFilter struct {
	Type   string
	Status string
	Page   int
	Limit  int
}

func (f *JobFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 20
	}
}

func (f JobFilter) Offset() int { return (f.Page - 1) * f.Limit }

// JobItemFilter 是逐项结果的查询条件。
type JobItemFilter struct {
	Status string
	Page   int
	Limit  int
}

func (f *JobItemFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	// 逐项结果可能有几千条，页大小放宽到 500——失败清单常常要一次看全。
	if f.Limit < 1 || f.Limit > 500 {
		f.Limit = 100
	}
}

func (f JobItemFilter) Offset() int { return (f.Page - 1) * f.Limit }

// RefreshLog 是一次令牌刷新的结果记录。
type RefreshLog struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	AccountID    string    `json:"account_id"`
	AccountEmail string    `json:"account_email"`
	JobID        string    `json:"job_id"`
	RefreshType  string    `json:"refresh_type"`
	Status       string    `json:"status"`
	ErrorKind    string    `json:"error_kind"`
	ErrorMessage string    `json:"error_message"`
	CreatedAt    time.Time `json:"created_at"`
}

// RefreshLogFilter 是刷新日志的查询条件。
type RefreshLogFilter struct {
	Status    string
	AccountID string
	JobID     string
	Page      int
	Limit     int
}

func (f *RefreshLogFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
}

func (f RefreshLogFilter) Offset() int { return (f.Page - 1) * f.Limit }

// RefreshStats 是 /mail/refresh/stats 的响应。
//
// 前四个数来自 mail_accounts 而不是日志表：它们回答的是「我的邮箱现在是什么状态」，
// 那是账号的属性，不是历史尝试次数的统计。
type RefreshStats struct {
	Total   int `json:"total"`
	Success int `json:"success"`
	Failed  int `json:"failed"`
	Never   int `json:"never"`
	// ByErrorKind 是最近一段时间内失败原因的分布，用来区分
	// 「一批账号被封」和「代理挂了」——两者的处置完全不同。
	ByErrorKind map[string]int `json:"by_error_kind"`
	LastJob     *Job           `json:"last_job"`
}
