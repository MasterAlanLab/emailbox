package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"emailbox/pkg/job"
	"emailbox/pkg/mailer"
	"emailbox/pkg/mailer/graph"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

// TokenRefresher 只做一件事：确认 refresh_token 还能换出 access_token。
// 抽成接口是为了让测试不必真的打微软。
type TokenRefresher interface {
	RefreshToken(ctx context.Context, cred mailer.Credential) error
}

// 批量刷新的选取范围。
const (
	RefreshScopeAll      = "all"
	RefreshScopeFailed   = "failed"
	RefreshScopeSelected = "selected"
)

// 刷新来源，写进 mail_refresh_logs.refresh_type。
const (
	RefreshTypeManual    = "manual"
	RefreshTypeJob       = "job"
	RefreshTypeScheduled = "scheduled"
)

// 失败原因统计的时间窗。
//
// 不加窗的话这条聚合会随历史增长退化成全表扫描；窗太短又会在
// 「昨晚跑的大批量」上什么都看不到。7 天覆盖了绝大多数排障场景。
const refreshFailureWindow = 7 * 24 * time.Hour

// ErrNoRefreshableAccounts 表示这批账号里没有一个能刷新的。
var ErrNoRefreshableAccounts = errors.New("没有可刷新的账号（需要 OAuth 账号且已保存 refresh_token）")

// RefreshService 负责令牌刷新：单个同步刷新、批量任务提交，以及作为
// job.Runner 被任务系统回调执行。
//
// 凭据组装（解密、代理继承）与写回（refresh_token 轮换、last_refresh_*、
// 封禁置位）全部复用 MessageService 的那一套。不另写一份的理由很实在：
// 两份实现迟早会在某次改动后产生差异，表现为「刷新说好了但拉信失败」，
// 而这种不一致极难排查。
type RefreshService struct {
	store    *repo.Store
	messages *MessageService
	quota    *quota.Service
	jobs     *job.Manager

	// refresherFor 按账号构造刷新器。默认走 Graph（只有 OAuth 账号需要刷新令牌）。
	refresherFor func(*model.MailAccount) TokenRefresher
}

func NewRefreshService(
	store *repo.Store, messages *MessageService, q *quota.Service, jobs *job.Manager,
) *RefreshService {
	s := &RefreshService{store: store, messages: messages, quota: q, jobs: jobs}
	s.refresherFor = s.defaultRefresher
	return s
}

// WithRefresherFactory 供测试注入假刷新器。
func (s *RefreshService) WithRefresherFactory(f func(*model.MailAccount) TokenRefresher) *RefreshService {
	s.refresherFor = f
	return s
}

// defaultRefresher 构造一个只做令牌交换的 Graph 客户端。
//
// 轮换回调脱离请求 context：令牌在上游已经换掉了，这时因为请求取消而不落库，
// 账号下次刷新必然失败——而且要等到下一轮任务才暴露。
func (s *RefreshService) defaultRefresher(account *model.MailAccount) TokenRefresher {
	return graph.New(graph.Config{
		OnTokenRefresh: func(_, refreshToken string) {
			s.messages.OnTokenRotated(
				context.WithoutCancel(context.Background()),
				account.TenantID, account.ID, refreshToken)
		},
	})
}

// Type 实现 job.Runner。
func (s *RefreshService) Type() string { return model.JobTypeTokenRefresh }

// Run 实现 job.Runner：任务系统按账号回调到这里。
func (s *RefreshService) Run(ctx context.Context, j model.Job, item model.JobItem) job.Result {
	if item.AccountID == "" {
		// 账号在任务排队期间被删了。这不是失败，跳过即可。
		return job.Result{Status: model.JobItemSkipped, Message: "账号已删除"}
	}
	err := s.refresh(ctx, j.TenantID, item.AccountID, j.ID, RefreshTypeJob)
	if err == nil {
		return job.Result{Status: model.JobItemSuccess}
	}
	return job.Result{
		Status:    model.JobItemFailed,
		ErrorKind: string(mailer.KindOf(err)),
		Message:   truncateError(err.Error()),
	}
}

// RefreshOne 同步刷新一个账号，供「单个刷新」端点使用。
func (s *RefreshService) RefreshOne(ctx context.Context, tenantID, accountID string) error {
	// 单个刷新也要扣配额：不扣的话，一个循环调用单刷接口的脚本可以绕开整个限额。
	if err := s.quota.CheckAndConsume(ctx, tenantID, model.MetricTokenRefresh, 1); err != nil {
		return err
	}
	return s.refresh(ctx, tenantID, accountID, "", RefreshTypeManual)
}

// refresh 是刷新的公共实现：取凭据 → 换令牌 → 写回结果 → 记日志。
func (s *RefreshService) refresh(ctx context.Context, tenantID, accountID, jobID, refreshType string) error {
	account, cred, err := s.messages.credential(ctx, tenantID, accountID)
	if err != nil {
		// 连凭据都取不到（账号被封/停用/密文解不开）也要留一条日志，
		// 否则用户在刷新日志里看不到这些账号，会以为它们「没被处理」。
		s.log(ctx, tenantID, accountID, "", jobID, refreshType, err)
		return err
	}
	if strings.TrimSpace(cred.RefreshToken) == "" {
		err := errors.New("账号没有 refresh_token，无需刷新")
		s.log(ctx, tenantID, accountID, account.Email, jobID, refreshType, err)
		return err
	}

	err = s.refresherFor(account).RefreshToken(ctx, cred)

	// 写回 last_refresh_* 并在识别到封禁时置账号状态。
	// 用脱离取消的 context：结果已经产生了，不该因为请求断开就丢失。
	s.messages.recordResult(context.WithoutCancel(ctx), tenantID, account, err)
	s.log(ctx, tenantID, accountID, account.Email, jobID, refreshType, err)
	return err
}

// log 写一条刷新日志。失败只记 slog：日志是旁路，不该让刷新本身失败。
func (s *RefreshService) log(ctx context.Context, tenantID, accountID, email, jobID, refreshType string, callErr error) {
	entry := model.RefreshLog{
		ID: uuid.NewString(), TenantID: tenantID, AccountID: accountID,
		AccountEmail: email, JobID: jobID, RefreshType: refreshType,
		Status: "success",
	}
	if callErr != nil {
		entry.Status = "failed"
		entry.ErrorKind = string(mailer.KindOf(callErr))
		entry.ErrorMessage = truncateError(callErr.Error())
	}
	if err := s.store.CreateRefreshLog(context.WithoutCancel(ctx), entry); err != nil {
		slog.Warn("写刷新日志失败", "account_id", accountID, "error", err)
	}
}

// SubmitBatch 提交一个批量刷新任务。
func (s *RefreshService) SubmitBatch(
	ctx context.Context, tenantID, userID, scope string, accountIDs []string,
) (*model.Job, error) {
	accounts, err := s.selectAccounts(ctx, tenantID, scope, accountIDs)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, ErrNoRefreshableAccounts
	}

	// 配额在提交时按账号数一次性预扣（08 文档 §4.2）：跑到一半再拒绝，
	// 等于让用户为半批结果付了全额，而且没法判断该从哪里续。
	if err := s.quota.CheckAndConsume(ctx, tenantID, model.MetricTokenRefresh, len(accounts)); err != nil {
		return nil, err
	}

	params := []byte("{}")
	if encoded, err := json.Marshal(map[string]any{"scope": scope, "count": len(accounts)}); err == nil {
		params = encoded
	}
	j := model.Job{
		ID: uuid.NewString(), TenantID: tenantID, Type: model.JobTypeTokenRefresh,
		Trigger: model.JobTriggerManual, Status: model.JobStatusPending,
		CreatedBy: userID, TotalCount: len(accounts), Params: string(params),
	}

	items := make([]model.JobItem, 0, len(accounts))
	for i, account := range accounts {
		items = append(items, model.JobItem{
			ID: uuid.NewString(), JobID: j.ID, AccountID: account.ID,
			Email: account.Email, Position: i, Status: model.JobItemPending,
		})
	}

	if err := s.jobs.Submit(ctx, j, items); err != nil {
		return nil, err
	}
	return &j, nil
}

// selectAccounts 按 scope 选出要刷新的账号。
//
// 三种 scope 都会再过一遍「有 refresh_token 且没被停用」：没有令牌的账号
// （IMAP 密码账号）刷新不了，放进任务里只会产出一堆注定失败的记录，
// 既浪费配额也把失败率搅乱。
func (s *RefreshService) selectAccounts(
	ctx context.Context, tenantID, scope string, accountIDs []string,
) ([]model.MailAccount, error) {
	var (
		accounts []model.MailAccount
		err      error
	)
	switch scope {
	case RefreshScopeSelected:
		if len(accountIDs) == 0 {
			return nil, errors.New("请先选择要刷新的账号")
		}
		accounts, err = s.store.ListMailAccountsByIDs(ctx, tenantID, accountIDs)
	case RefreshScopeFailed:
		filter := model.AccountFilter{RefreshStatus: string(model.RefreshFailed)}
		filter.Limit = maxBatchAccounts
		filter.Normalize()
		accounts, err = s.store.ListMailAccountsPage(ctx, tenantID, filter)
	case RefreshScopeAll, "":
		filter := model.AccountFilter{}
		filter.Limit = maxBatchAccounts
		filter.Normalize()
		accounts, err = s.store.ListMailAccountsPage(ctx, tenantID, filter)
	default:
		return nil, fmt.Errorf("未知的选取范围 %q", scope)
	}
	if err != nil {
		return nil, err
	}

	out := make([]model.MailAccount, 0, len(accounts))
	for _, account := range accounts {
		if account.RefreshTokenEnc == "" || account.Status != model.AccountStatusActive {
			continue
		}
		out = append(out, account)
	}
	return out, nil
}

// maxBatchAccounts 是一个任务能包含的账号数上限。
// 5000 个账号按 8 并发、每个 1.5 秒算约 16 分钟，再多就该拆成多个任务了。
const maxBatchAccounts = 5000

// Stats 返回刷新概况：账号当前状态分布 + 最近失败原因分布 + 最后一个任务。
func (s *RefreshService) Stats(ctx context.Context, tenantID string) (*model.RefreshStats, error) {
	stats, err := s.store.GetRefreshStats(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	kinds, err := s.store.GroupRefreshFailures(ctx, tenantID, time.Now().Add(-refreshFailureWindow))
	if err != nil {
		return nil, err
	}
	stats.ByErrorKind = kinds

	jobs, _, err := s.store.ListJobs(ctx, tenantID, model.JobFilter{
		Type: model.JobTypeTokenRefresh, Page: 1, Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(jobs) > 0 {
		stats.LastJob = &jobs[0]
	}
	return stats, nil
}

// Logs 分页取刷新日志。
func (s *RefreshService) Logs(ctx context.Context, tenantID string, f model.RefreshLogFilter) ([]model.RefreshLog, int, error) {
	return s.store.ListRefreshLogs(ctx, tenantID, f)
}
