package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"emailbox/pkg/job"
	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

// 账号检测的选取范围。invalid 用于删除前复核：失效结论可能是几天前得出的。
const (
	CheckScopeAll      = "all"
	CheckScopeGroup    = "group"
	CheckScopeSelected = "selected"
	CheckScopeInvalid  = "invalid"
)

var (
	ErrNoCheckableAccounts = errors.New("没有可检测的账号（已停用的账号不参与检测）")
	ErrCheckJobRunning     = errors.New("已有账号检测任务在运行，请等它结束后再提交")
	ErrNoInvalidAccounts   = errors.New("当前没有失效账号")
	// ErrInvalidCountChanged 表示确认之后失效账号数变了。409 让前端重新取数再确认一次，
	// 而不是替用户删掉一批他没看到过的账号。
	ErrInvalidCountChanged = errors.New("失效账号数量已变化，请刷新后重新确认")
)

// healthOf 把一次访问的结果翻译成健康结论。verdict=false 表示这次得不出结论，不写库。
//
// invalid 只收「账号自身」的失败：被封、邮箱拒绝连接、授权失效。网络、代理、限流、
// 服务商故障一律记 error——它们换个时间或出口就可能好，「删除失效账号」绝不能删到它们。
// 凭据解不开（ENCRYPTION_KEY 换过）同样是 error：那是部署问题，账号本身没坏。
func healthOf(err error) (status model.HealthStatus, kind string, verdict bool) {
	switch {
	case err == nil:
		return model.HealthOK, "", true
	case errors.Is(err, ErrAccountBanned):
		return model.HealthInvalid, string(mailer.ErrKindBanned), true
	case errors.Is(err, ErrAccountDisabled), errors.Is(err, repo.ErrNotFound):
		// 停用是用户自己的选择，账号可能完全正常。
		return "", "", false
	}
	switch k := mailer.KindOf(err); k {
	case mailer.ErrKindBanned, mailer.ErrKindAccountUnavailable, mailer.ErrKindAuthFailed:
		return model.HealthInvalid, string(k), true
	case mailer.ErrKindFolderUnavailable:
		// 登录已经成功，只是某个邮件夹或某封信不在。
		return model.HealthOK, "", true
	case mailer.ErrKindCanceled:
		return "", "", false
	default:
		return model.HealthError, string(k), true
	}
}

// HealthService 负责账号有效性检测与失效账号清理。
//
// 检测就是「真正登录一次邮箱」：取收件箱最新一封的信封。它复用 MessageService 的
// 凭据组装、回退链与结果写回，和收信走的是同一条路——两条路径一旦分叉，
// 就会重现「检测说好了、收信却失败」这种最难排查的不一致。
//
// 检测不扣取件额度：它不向用户返回任何邮件内容，而它回答的「账号还能不能用」
// 与令牌刷新一样是托管的前提。批量对上游的压力由 JOB_WORKERS / JOB_ACCOUNT_DELAY_MS 控制。
type HealthService struct {
	store    *repo.Store
	messages *MessageService
	jobs     *job.Manager
}

func NewHealthService(store *repo.Store, messages *MessageService, jobs *job.Manager) *HealthService {
	return &HealthService{store: store, messages: messages, jobs: jobs}
}

// Type 实现 job.Runner。
func (s *HealthService) Type() string { return model.JobTypeAccountCheck }

// Run 实现 job.Runner：检测一个账号。
func (s *HealthService) Run(ctx context.Context, j model.Job, item model.JobItem) job.Result {
	if item.AccountID == "" {
		return job.Result{Status: model.JobItemSkipped, Message: "账号已删除"}
	}
	err := s.check(ctx, j.TenantID, item.AccountID)
	switch {
	case err == nil:
		return job.Result{Status: model.JobItemSuccess}
	case errors.Is(err, ErrAccountDisabled):
		return job.Result{Status: model.JobItemSkipped, Message: "账号已停用"}
	case errors.Is(err, repo.ErrNotFound):
		return job.Result{Status: model.JobItemSkipped, Message: "账号已删除"}
	}
	kind := string(mailer.KindOf(err))
	if errors.Is(err, ErrAccountBanned) {
		kind = string(mailer.ErrKindBanned)
	}
	return job.Result{Status: model.JobItemFailed, ErrorKind: kind, Message: truncateError(err.Error())}
}

// check 登录一次邮箱并写回结论。
func (s *HealthService) check(ctx context.Context, tenantID, accountID string) error {
	account, cred, err := s.messages.credential(ctx, tenantID, accountID)
	if err != nil {
		// 没走到上游的失败也要落结论：已标记封禁的账号正是最该出现在失效清单里的那批。
		s.recordLocalFailure(ctx, tenantID, accountID, err)
		return err
	}
	client, record := s.messages.clientFor(ctx, tenantID, account)
	_, err = client.List(ctx, cred, mailer.ListOptions{Folder: mailer.FolderInbox, Top: 1})
	record(err)
	return err
}

func (s *HealthService) recordLocalFailure(ctx context.Context, tenantID, accountID string, callErr error) {
	status, kind, verdict := healthOf(callErr)
	if !verdict {
		return
	}
	if err := s.store.UpdateMailAccountHealth(ctxDetached(ctx), tenantID, accountID,
		string(status), kind, truncateError(callErr.Error())); err != nil && !errors.Is(err, repo.ErrNotFound) {
		slog.Warn("写回账号检测结果失败", "account_id", accountID, "error", err)
	}
}

// SubmitBatch 提交一个账号检测任务。
func (s *HealthService) SubmitBatch(
	ctx context.Context, tenantID, userID, scope string, accountIDs, groupIDs []string,
) (*model.Job, error) {
	// 同一租户同时只跑一个检测任务：两个任务对同一批账号并发登录，
	// 上游看到的是翻倍的请求量，风控只会更快落下来。
	active, err := s.store.CountActiveJobsByType(ctx, tenantID, model.JobTypeAccountCheck)
	if err != nil {
		return nil, err
	}
	if active > 0 {
		return nil, ErrCheckJobRunning
	}

	accounts, err := s.selectAccounts(ctx, tenantID, scope, accountIDs, groupIDs)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return nil, ErrNoCheckableAccounts
	}

	fields := map[string]any{"scope": scope, "count": len(accounts)}
	if scope == CheckScopeGroup {
		fields["group_ids"] = groupIDs
	}
	params := []byte("{}")
	if encoded, err := json.Marshal(fields); err == nil {
		params = encoded
	}
	j := model.Job{
		ID: uuid.NewString(), TenantID: tenantID, Type: model.JobTypeAccountCheck,
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

// selectAccounts 按 scope 选出要检测的账号。与令牌刷新不同，IMAP 密码账号也参与：
// 它们没有令牌可换，但同样可能因为改了密码、关了 IMAP 而失效。停用的账号跳过。
func (s *HealthService) selectAccounts(
	ctx context.Context, tenantID, scope string, accountIDs, groupIDs []string,
) ([]model.MailAccount, error) {
	var (
		accounts []model.MailAccount
		err      error
	)
	switch scope {
	case CheckScopeSelected:
		if len(accountIDs) == 0 {
			return nil, errors.New("请先选择要检测的账号")
		}
		if len(accountIDs) > model.MaxBatchAccountIDs {
			return nil, fmt.Errorf("单次最多检测 %d 个账号，请分批", model.MaxBatchAccountIDs)
		}
		accounts, err = s.store.ListMailAccountsByIDs(ctx, tenantID, accountIDs)
	case CheckScopeGroup:
		accounts, err = accountsInGroups(ctx, s.store, tenantID, groupIDs)
	case CheckScopeInvalid:
		accounts, err = collectAccounts(ctx, s.store, tenantID,
			model.AccountFilter{HealthStatus: string(model.HealthInvalid)})
	case CheckScopeAll, "":
		accounts, err = collectAccounts(ctx, s.store, tenantID, model.AccountFilter{})
	default:
		return nil, fmt.Errorf("未知的选取范围 %q", scope)
	}
	if err != nil {
		return nil, err
	}
	out := make([]model.MailAccount, 0, len(accounts))
	for _, account := range accounts {
		if account.Status == model.AccountStatusDisabled {
			continue
		}
		out = append(out, account)
	}
	return out, nil
}

// Stats 返回账号可用性分布与最后一个检测任务。groupID 为空表示全部分组。
func (s *HealthService) Stats(ctx context.Context, tenantID, groupID string) (*model.HealthStats, error) {
	if groupID != "" {
		if _, err := s.store.GetMailGroup(ctx, tenantID, groupID); err != nil {
			return nil, err
		}
	}
	rows, err := s.store.CountMailAccountsByHealth(ctx, tenantID, groupID)
	if err != nil {
		return nil, err
	}
	stats := &model.HealthStats{InvalidByKind: map[string]int{}}
	for _, r := range rows {
		stats.Total += r.Count
		switch r.Status {
		case model.HealthOK:
			stats.OK += r.Count
		case model.HealthInvalid:
			stats.Invalid += r.Count
			stats.InvalidByKind[r.ErrorKind] += r.Count
		case model.HealthError:
			stats.Error += r.Count
		default:
			stats.Unknown += r.Count
		}
	}

	jobs, _, err := s.store.ListJobs(ctx, tenantID, model.JobFilter{
		Type: model.JobTypeAccountCheck, Page: 1, Limit: 1,
	})
	if err != nil {
		return nil, err
	}
	if len(jobs) > 0 {
		stats.LastJob = &jobs[0]
	}
	return stats, nil
}

// DeleteInvalid 软删除失效账号（清空凭据），返回删除数。
//
// 删的是「语句执行那一刻」仍为 invalid 的账号，而不是之前选出的 ID 列表：
// 检测任务可能正在把其中某个改回 ok。Expected 核对的是另一个方向——
// 确认之后又多出来的失效账号，不能在用户没看到的情况下一起删掉。
func (s *HealthService) DeleteInvalid(ctx context.Context, tenantID string, req model.DeleteInvalidRequest) (int, error) {
	stats, err := s.Stats(ctx, tenantID, req.GroupID)
	if err != nil {
		return 0, err
	}
	if stats.Invalid == 0 {
		return 0, ErrNoInvalidAccounts
	}
	if req.Expected != stats.Invalid {
		return 0, ErrInvalidCountChanged
	}
	return s.store.SoftDeleteInvalidMailAccounts(ctx, tenantID, req.GroupID)
}
