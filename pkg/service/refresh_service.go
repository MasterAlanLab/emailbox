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
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

// TokenRefresher 与协议层共用契约，保留 service 的测试注入入口。
type TokenRefresher = mailer.TokenRefresher

// 批量刷新的选取范围。
const (
	RefreshScopeAll      = "all"
	RefreshScopeFailed   = "failed"
	RefreshScopeSelected = "selected"
	RefreshScopeGroup    = "group"
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

	// refresherFor 与收信共用 OAuth 通道链，优先上次成功的通道。
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

// defaultRefresher 复用收信的通道配置、轮换和成功通道写回，只执行令牌交换。
func (s *RefreshService) defaultRefresher(account *model.MailAccount) TokenRefresher {
	return newMailChain(s.messages, account, s.messages.chainOptions)
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
	// 只记账，不设限。令牌刷新是「账号还能不能用」的前提：卡住它，用户看到的
	// 不是「今天少刷点」，而是一批账号集体登录失败——那个后果比省下的上游调用重得多。
	// 记账仍然要做，用量页上的这个数字是判断「是不是有脚本在空转」的唯一线索。
	if err := s.quota.Record(ctx, tenantID, model.MetricTokenRefresh, 1); err != nil {
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
	ctx context.Context, tenantID, userID, scope string, accountIDs, groupIDs []string,
) (*model.Job, error) {
	accounts, err := s.selectAccounts(ctx, tenantID, scope, accountIDs, groupIDs)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, ErrNoRefreshableAccounts
	}

	// 按账号数一次性记账（不设限，理由见 RefreshOne）。
	if err := s.quota.Record(ctx, tenantID, model.MetricTokenRefresh, len(accounts)); err != nil {
		return nil, err
	}

	fields := map[string]any{"scope": scope, "count": len(accounts)}
	// 记下分组：任务列表里三条 scope=group 的任务，不写这个就分不出刷的是谁。
	if scope == RefreshScopeGroup {
		fields["group_ids"] = groupIDs
	}
	params := []byte("{}")
	if encoded, err := json.Marshal(fields); err == nil {
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
// 四种 scope 都会再过一遍「Outlook OAuth、有 refresh_token 且没被停用」：
// IMAP 密码账号没有 OAuth 令牌可换，放进任务只会产出一堆注定失败的记录，
// 既浪费配额也把失败率搅乱。
func (s *RefreshService) selectAccounts(
	ctx context.Context, tenantID, scope string, accountIDs, groupIDs []string,
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
	case RefreshScopeGroup:
		accounts, err = s.accountsInGroups(ctx, tenantID, groupIDs)
	case RefreshScopeFailed:
		accounts, err = s.collectAccounts(ctx, tenantID,
			model.AccountFilter{RefreshStatus: string(model.RefreshFailed)})
	case RefreshScopeAll, "":
		accounts, err = s.collectAccounts(ctx, tenantID, model.AccountFilter{})
	default:
		return nil, fmt.Errorf("未知的选取范围 %q", scope)
	}
	if err != nil {
		return nil, err
	}

	out := make([]model.MailAccount, 0, len(accounts))
	for _, account := range accounts {
		if account.AccountType != string(mailer.AccountTypeOutlook) || account.RefreshTokenEnc == "" || account.Status != model.AccountStatusActive {
			continue
		}
		out = append(out, account)
	}
	return out, nil
}

// accountsInGroups 取出若干分组下的账号。分组先校验归属，避免用别人的
// group_id 探测到不属于自己的账号。
func (s *RefreshService) accountsInGroups(
	ctx context.Context, tenantID string, groupIDs []string,
) ([]model.MailAccount, error) {
	if len(groupIDs) == 0 {
		return nil, errors.New("请先选择要刷新的分组")
	}
	groups, err := s.store.ListMailGroups(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(groups))
	for _, g := range groups {
		known[g.ID] = true
	}

	// 列表查询一次只认一个分组（筛选 SQL 里没有变长 IN），所以逐个分组取再合并。
	// 同 AccountService.exportTargets 的做法。
	out := make([]model.MailAccount, 0)
	seen := map[string]bool{}
	for _, gid := range groupIDs {
		if !known[gid] {
			return nil, fmt.Errorf("分组 %s 不存在", gid)
		}
		if seen[gid] {
			continue
		}
		seen[gid] = true
		batch, err := s.collectAccounts(ctx, tenantID, model.AccountFilter{GroupIDs: []string{gid}})
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
	}
	return out, nil
}

// collectAccounts 按筛选条件翻页取全量，最多 maxBatchAccounts 个。
//
// 必须翻页：AccountFilter.Normalize 会把 Limit 压到 MaxAccountPageSize(200)，
// 「填一个大 Limit 一次取完」的写法拿到的永远只有前 200 个。
func (s *RefreshService) collectAccounts(
	ctx context.Context, tenantID string, filter model.AccountFilter,
) ([]model.MailAccount, error) {
	filter.Limit = model.MaxAccountPageSize
	filter.Normalize()
	out := make([]model.MailAccount, 0, filter.Limit)
	for page := 1; len(out) < maxBatchAccounts; page++ {
		filter.Page = page
		batch, err := s.store.ListMailAccountsPage(ctx, tenantID, filter)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(batch) < filter.Limit {
			break
		}
	}
	if len(out) > maxBatchAccounts {
		out = out[:maxBatchAccounts]
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
