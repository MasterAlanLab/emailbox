package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
)

// Export 把账号导出成可被本平台重新导入的文本。
//
// 这是全平台风险最高的接口：一次成功的调用等于取走该租户全部邮箱的凭据明文。
// 三道闸门缺一不可，且分布在三层——
//   - 权限 account:secret（路由）
//   - 强制审计（路由上的 AuditWrite）
//   - 按用户限流（路由上的 exportLimiter）
//
// 这里只负责第四件事：SQL 一律带 tenant_id，管理员跨租户导出也照样落在目标租户上。
func (s *AccountService) Export(ctx context.Context, tenantID string, req model.ExportAccountsRequest) (string, int, error) {
	accounts, err := s.exportTargets(ctx, tenantID, req)
	if err != nil {
		return "", 0, err
	}

	var buf strings.Builder
	count := 0
	for i := range accounts {
		cred, err := s.decryptCredentials(&accounts[i])
		if err != nil {
			return "", 0, err
		}
		line, err := mailer.FormatLine(mailer.ParsedAccount{
			Email: cred.Email, Password: firstNonEmpty(cred.Password, cred.IMAPPassword),
			ClientID: cred.ClientID, RefreshToken: cred.RefreshToken,
			IMAPHost: cred.IMAPHost, IMAPPort: cred.IMAPPort,
		})
		if err != nil {
			// 没有凭据的账号（例如导入时只有邮箱那一列）导出来也无法再导入，
			// 跳过而不是让整次导出失败——用户要的是另外那几千个能用的。
			continue
		}
		buf.WriteString(line)
		buf.WriteString("\n")
		count++
	}
	return buf.String(), count, nil
}

// exportTargets 按范围取出待导出的账号，全部经过 tenant_id 过滤。
func (s *AccountService) exportTargets(ctx context.Context, tenantID string, req model.ExportAccountsRequest) ([]model.MailAccount, error) {
	switch req.Scope {
	case model.ExportScopeSelected:
		if err := validateBatchIDs(req.AccountIDs); err != nil {
			return nil, err
		}
		return s.store.ListMailAccountsByIDs(ctx, tenantID, req.AccountIDs)
	case model.ExportScopeGroup:
		groupIDs, err := s.checkGroups(ctx, tenantID, req.GroupIDs)
		if err != nil {
			return nil, err
		}
		// 列表查询只认一个分组（双引擎的筛选 SQL 各写一份，变长 IN 只用在批量取
		// 标签与别名上），所以逐个分组取再合并。
		out := make([]model.MailAccount, 0)
		seen := map[string]bool{}
		for _, gid := range groupIDs {
			batch, err := s.collectAccounts(ctx, tenantID, model.AccountFilter{GroupIDs: []string{gid}})
			if err != nil {
				return nil, err
			}
			for _, a := range batch {
				if !seen[a.ID] {
					seen[a.ID] = true
					out = append(out, a)
				}
			}
		}
		if len(out) > model.MaxExportAccounts {
			return nil, errExportTooMany()
		}
		return out, nil
	case model.ExportScopeAll:
		return s.collectAccounts(ctx, tenantID, model.AccountFilter{})
	default:
		return nil, fmt.Errorf("scope 取值非法，应为 %s / %s / %s",
			model.ExportScopeAll, model.ExportScopeGroup, model.ExportScopeSelected)
	}
}

// checkGroups 校验请求里的分组都属于本租户，并去掉重复项。
func (s *AccountService) checkGroups(ctx context.Context, tenantID string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, errors.New("group_ids 不能为空")
	}
	groups, err := s.store.ListMailGroups(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	known := make(map[string]bool, len(groups))
	for _, g := range groups {
		known[g.ID] = true
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !known[id] {
			return nil, fmt.Errorf("分组 %s 不存在", id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, nil
}

// collectAccounts 逐页取账号直到取完或触到上限。
// 复用列表分页而不是新写一条「取全部」的 SQL：筛选条件的双引擎写法只维护一份，
// 两边漂移的风险就少一处。
func (s *AccountService) collectAccounts(ctx context.Context, tenantID string, filter model.AccountFilter) ([]model.MailAccount, error) {
	filter.Limit = model.MaxAccountPageSize
	out := make([]model.MailAccount, 0, filter.Limit)
	for page := 1; ; page++ {
		filter.Page = page
		batch, err := s.store.ListMailAccountsPage(ctx, tenantID, filter)
		if err != nil {
			return nil, err
		}
		out = append(out, batch...)
		if len(out) > model.MaxExportAccounts {
			return nil, errExportTooMany()
		}
		if len(batch) < filter.Limit {
			return out, nil
		}
	}
}

func errExportTooMany() error {
	return fmt.Errorf("单次最多导出 %d 个账号，请按分组分批导出", model.MaxExportAccounts)
}
