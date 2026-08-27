package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"

	"github.com/google/uuid"
)

// Import 批量导入账号。
//
// 逐行统计而非全成功/全失败：用户一次粘几千行，因为其中几行有问题就整批回滚，
// 体验极差且浪费上游调用。配额超出的部分同理——计入 skipped 并在响应里说明原因，
// 已导入的保留。
//
// 按 importBatchSize 分批提交：整批一个事务会长时间持锁，
// SQLite 只有一个连接，会直接卡死其它所有请求。
func (s *AccountService) Import(ctx context.Context, tenantID string, req model.ImportAccountsRequest) (*model.ImportResult, error) {
	plan, used, err := s.prepareImport(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}

	lines := strings.Split(req.Content, "\n")
	result := &model.ImportResult{Errors: []model.ImportError{}}
	// 同一份内容里出现重复邮箱时，后面的行按 skipped 处理，
	// 否则会在唯一索引上失败并被计成 failed，看起来像是数据有问题。
	seen := map[string]bool{}

	batch := make([]pendingAccount, 0, importBatchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		err := s.store.WithTx(ctx, func(tx *repo.Store) error {
			for _, p := range batch {
				if err := s.persistImported(ctx, tx, p); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			// 整批失败时如实反映：这一批全部计入 failed，之前已提交的批次不受影响。
			for _, p := range batch {
				result.Failed++
				result.AddError(p.line, p.parsed.Email, "写入失败: "+err.Error())
			}
			result.Created -= countNew(batch)
			result.Updated -= len(batch) - countNew(batch)
		}
		batch = batch[:0]
	}

	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		result.Total++
		pending, ok := s.planLine(ctx, plan, i+1, line, seen, &used, result)
		if !ok {
			continue
		}
		batch = append(batch, pending)
		if len(batch) >= importBatchSize {
			flush()
		}
	}
	flush()
	return result, nil
}

// prepareImport 做整批只需一次的校验与取值，并返回当前账号数作为配额起点。
func (s *AccountService) prepareImport(ctx context.Context, tenantID string, req model.ImportAccountsRequest) (importPlan, int, error) {
	group, err := s.resolveGroup(ctx, tenantID, req.GroupID)
	if err != nil {
		return importPlan{}, 0, err
	}
	status := req.Defaults.Status
	if status == "" {
		status = model.AccountStatusActive
	}
	if !model.ValidAccountStatus(status) {
		return importPlan{}, 0, errors.New("账号状态取值非法")
	}
	opts := mailer.ImportOptions{
		Format:        mailer.ImportFormat(req.Format),
		ClientIDFirst: true,
		IMAPHost:      strings.TrimSpace(req.IMAPHost),
		IMAPPort:      req.IMAPPort,
	}
	if req.ClientIDFirst != nil {
		opts.ClientIDFirst = *req.ClientIDFirst
	}
	limits, err := s.quota.Effective(ctx, tenantID)
	if err != nil {
		return importPlan{}, 0, err
	}
	used, err := s.store.CountMailAccounts(ctx, tenantID)
	if err != nil {
		return importPlan{}, 0, err
	}
	return importPlan{
		tenantID: tenantID, groupID: group.ID, status: status,
		opts: opts, req: req, updateOnConflict: req.OnConflict == "update",
		maxAccounts: limits.MaxAccounts,
	}, used, nil
}

// importPlan 收拢逐行处理需要的、整批不变的参数。
type importPlan struct {
	tenantID         string
	groupID          string
	status           model.AccountStatus
	opts             mailer.ImportOptions
	req              model.ImportAccountsRequest
	updateOnConflict bool
	maxAccounts      int
}

// planLine 处理一行：解析、判重、查冲突、扣配额、组装。
// 返回 ok=false 表示该行已在 result 里记过账（skipped 或 failed），不进批次。
func (s *AccountService) planLine(
	ctx context.Context, plan importPlan, lineNo int, line string,
	seen map[string]bool, used *int, result *model.ImportResult,
) (pendingAccount, bool) {
	parsed, err := mailer.ParseLine(line, plan.opts)
	if err != nil {
		result.Failed++
		result.AddError(lineNo, "", err.Error())
		return pendingAccount{}, false
	}
	if seen[parsed.Email] {
		result.Skipped++
		result.AddError(lineNo, parsed.Email, "本次导入内容中重复")
		return pendingAccount{}, false
	}
	seen[parsed.Email] = true

	existing, err := s.store.GetMailAccountByEmail(ctx, plan.tenantID, parsed.Email)
	switch {
	case err == nil && !plan.updateOnConflict:
		result.Skipped++
		result.AddError(lineNo, parsed.Email, "邮箱已存在")
		return pendingAccount{}, false
	case err != nil && !errors.Is(err, repo.ErrNotFound):
		result.Failed++
		result.AddError(lineNo, parsed.Email, err.Error())
		return pendingAccount{}, false
	}

	isNew := err != nil
	if isNew {
		// 配额只约束新增。超额部分计入 skipped 而不是让整批失败——
		// 用户一次粘几千行，因为超了 3 个就整批拒绝的体验极差。
		if quota.Allowance(plan.maxAccounts, *used, 1) == 0 {
			result.Skipped++
			result.AddError(lineNo, parsed.Email, fmt.Sprintf("超出账号配额（上限 %d）", plan.maxAccounts))
			return pendingAccount{}, false
		}
		*used++
	}

	account, buildErr := s.buildImported(parsed, plan.tenantID, plan.groupID, plan.status, plan.req, existing)
	if buildErr != nil {
		result.Failed++
		result.AddError(lineNo, parsed.Email, buildErr.Error())
		if isNew {
			*used--
		}
		return pendingAccount{}, false
	}
	if isNew {
		result.Created++
	} else {
		result.Updated++
	}
	return pendingAccount{line: lineNo, parsed: parsed, account: account, isNew: isNew}, true
}

// pendingAccount 是一条等待写库的导入结果。
type pendingAccount struct {
	line    int
	parsed  *mailer.ParsedAccount
	account *model.MailAccount
	isNew   bool
}

func countNew(batch []pendingAccount) int {
	n := 0
	for _, p := range batch {
		if p.isNew {
			n++
		}
	}
	return n
}

// buildImported 把解析结果组装成待写库的账号（凭据已加密）。
func (s *AccountService) buildImported(
	parsed *mailer.ParsedAccount, tenantID, groupID string,
	status model.AccountStatus, req model.ImportAccountsRequest, existing *model.MailAccount,
) (*model.MailAccount, error) {
	account := &model.MailAccount{
		ID: uuid.NewString(), TenantID: tenantID, GroupID: groupID,
		Email: parsed.Email, EmailNormalized: parsed.Email,
		Provider: parsed.Provider, AccountType: string(parsed.AccountType),
		ClientID: parsed.ClientID,
		IMAPHost: parsed.IMAPHost, IMAPPort: parsed.IMAPPort,
		Status: status, Remark: strings.TrimSpace(req.Defaults.Remark),
	}
	if existing != nil {
		// 覆盖模式下保留原有的 ID、分组与备注：用户可能已经整理过分组，
		// 重新导入一份凭据不该把这些整理成果冲掉。
		account.ID = existing.ID
		account.GroupID = existing.GroupID
		if req.Defaults.Remark == "" {
			account.Remark = existing.Remark
		}
		account.SortOrder = existing.SortOrder
	}

	// IMAP 账号的授权码存进 imap_password_enc；OAuth 账号的登录密码存进 password_enc。
	password, imapPassword := "", ""
	if parsed.AccountType == mailer.AccountTypeIMAP {
		imapPassword = parsed.Password
	} else {
		password = parsed.Password
	}
	if err := s.encryptInto(account, password, parsed.RefreshToken, imapPassword, "", "", ""); err != nil {
		return nil, err
	}
	if existing != nil {
		// 代理配置不在导入内容里，保留原值而不是清空。
		account.ProxyURL = existing.ProxyURL
		account.FallbackProxyURL1 = existing.FallbackProxyURL1
		account.FallbackProxyURL2 = existing.FallbackProxyURL2
	}
	return account, nil
}

func (s *AccountService) persistImported(ctx context.Context, tx *repo.Store, p pendingAccount) error {
	if p.isNew {
		return tx.CreateMailAccount(ctx, p.account)
	}
	return tx.UpdateMailAccount(ctx, p.account)
}

// BatchMove 把账号移动到另一个分组。
func (s *AccountService) BatchMove(ctx context.Context, tenantID string, req model.BatchMoveRequest) (*model.BatchResult, error) {
	if err := validateBatchIDs(req.AccountIDs); err != nil {
		return nil, err
	}
	group, err := s.resolveGroup(ctx, tenantID, req.GroupID)
	if err != nil {
		return nil, err
	}
	n, err := s.store.BatchMoveMailAccounts(ctx, tenantID, group.ID, req.AccountIDs)
	return batchResult(req.AccountIDs, n, err)
}

// BatchStatus 批量改状态。
func (s *AccountService) BatchStatus(ctx context.Context, tenantID string, req model.BatchStatusRequest) (*model.BatchResult, error) {
	if err := validateBatchIDs(req.AccountIDs); err != nil {
		return nil, err
	}
	if !model.ValidAccountStatus(req.Status) {
		return nil, errors.New("账号状态取值非法")
	}
	n, err := s.store.BatchUpdateMailAccountStatus(ctx, tenantID, string(req.Status), req.AccountIDs)
	return batchResult(req.AccountIDs, n, err)
}

// BatchProxy 批量设置代理。代理串含认证口令，整串加密后落库。
func (s *AccountService) BatchProxy(ctx context.Context, tenantID string, req model.BatchProxyRequest) (*model.BatchResult, error) {
	if err := validateBatchIDs(req.AccountIDs); err != nil {
		return nil, err
	}
	encrypted := make([]string, 3)
	for i, raw := range []string{req.ProxyURL, req.FallbackProxyURL1, req.FallbackProxyURL2} {
		enc, err := s.cipher.Encrypt(strings.TrimSpace(raw))
		if err != nil {
			return nil, fmt.Errorf("加密代理地址失败: %w", err)
		}
		encrypted[i] = enc
	}
	n, err := s.store.BatchUpdateMailAccountProxy(ctx, tenantID, encrypted[0], encrypted[1], encrypted[2], req.AccountIDs)
	return batchResult(req.AccountIDs, n, err)
}

// BatchDelete 批量软删除。凭据密文由 SQL 在同一条语句里清空。
func (s *AccountService) BatchDelete(ctx context.Context, tenantID string, req model.BatchDeleteRequest) (*model.BatchResult, error) {
	if err := validateBatchIDs(req.AccountIDs); err != nil {
		return nil, err
	}
	n, err := s.store.BatchSoftDeleteMailAccounts(ctx, tenantID, req.AccountIDs)
	return batchResult(req.AccountIDs, n, err)
}

func validateBatchIDs(ids []string) error {
	if len(ids) == 0 {
		return errors.New("account_ids 不能为空")
	}
	if len(ids) > model.MaxBatchAccountIDs {
		return fmt.Errorf("单次最多操作 %d 个账号，请分批", model.MaxBatchAccountIDs)
	}
	return nil
}

// batchResult 把「影响行数」翻译成统一的批量返回。
// 不存在或属于其它租户的 ID 不会被 SQL 命中，因此自然落进 failed。
func batchResult(ids []string, affected int, err error) (*model.BatchResult, error) {
	if err != nil {
		return nil, err
	}
	return &model.BatchResult{
		Requested: len(ids),
		Succeeded: affected,
		Failed:    len(ids) - affected,
		Errors:    []model.BatchError{},
	}, nil
}
