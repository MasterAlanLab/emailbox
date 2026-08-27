package repo

import (
	"context"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
)

func (s *Store) CreateMailAlias(ctx context.Context, id, tenantID, accountID, aliasEmail, aliasNormalized string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.CreateMailAlias(ctx, sqlitedb.CreateMailAliasParams{
			ID: id, TenantID: tenantID, AccountID: accountID,
			AliasEmail: aliasEmail, AliasNormalized: aliasNormalized,
		}))
	}
	return normalize(s.postgres.CreateMailAlias(ctx, postgresdb.CreateMailAliasParams{
		ID: id, TenantID: tenantID, AccountID: accountID,
		AliasEmail: aliasEmail, AliasNormalized: aliasNormalized,
	}))
}

func (s *Store) DeleteMailAliasesByAccount(ctx context.Context, tenantID, accountID string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.DeleteMailAliasesByAccount(ctx, sqlitedb.DeleteMailAliasesByAccountParams{
			TenantID: tenantID, AccountID: accountID,
		}))
	}
	return normalize(s.postgres.DeleteMailAliasesByAccount(ctx, postgresdb.DeleteMailAliasesByAccountParams{
		TenantID: tenantID, AccountID: accountID,
	}))
}

// ListMailAliases 一次取回多个账号的别名，返回 accountID -> aliases，避免 N+1。
func (s *Store) ListMailAliases(ctx context.Context, tenantID string, accountIDs []string) (map[string][]string, error) {
	result := map[string][]string{}
	if len(accountIDs) == 0 {
		return result, nil
	}
	if err := checkInList(accountIDs); err != nil {
		return nil, err
	}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListMailAliasesByAccountIDs(ctx, sqlitedb.ListMailAliasesByAccountIDsParams{
			TenantID: tenantID, AccountIds: accountIDs,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			result[r.AccountID] = append(result[r.AccountID], r.AliasEmail)
		}
		return result, nil
	}
	rows, err := s.postgres.ListMailAliasesByAccountIDs(ctx, postgresdb.ListMailAliasesByAccountIDsParams{
		TenantID: tenantID, Column2: accountIDs,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		result[r.AccountID] = append(result[r.AccountID], r.AliasEmail)
	}
	return result, nil
}

// CountConflictingAliases 统计这批别名里有多少已经属于**其它**账号。
// 别名的唯一索引建在 (tenant_id, alias_normalized) 上，这里提前检查是为了
// 给出「哪个别名冲突」的具体提示，而不是让用户面对一条数据库唯一约束错误。
func (s *Store) CountConflictingAliases(ctx context.Context, tenantID, accountID string, aliasesNormalized []string) (int, error) {
	if len(aliasesNormalized) == 0 {
		return 0, nil
	}
	if err := checkInList(aliasesNormalized); err != nil {
		return 0, err
	}
	if s.driver == "sqlite" {
		n, e := s.sqlite.CountConflictingAliases(ctx, sqlitedb.CountConflictingAliasesParams{
			TenantID: tenantID, AccountID: accountID, Aliases: aliasesNormalized,
		})
		return int(n), normalize(e)
	}
	n, e := s.postgres.CountConflictingAliases(ctx, postgresdb.CountConflictingAliasesParams{
		TenantID: tenantID, AccountID: accountID, Column3: aliasesNormalized,
	})
	return int(n), normalize(e)
}
