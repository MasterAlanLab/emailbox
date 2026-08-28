package repo

import (
	"context"
	"fmt"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// maxInListSize 是单条 SQL 里变长 IN 列表的元素上限。
// json_each / = ANY 本身不受 999 变量上限约束，但超大参数会拖垮查询计划。
const maxInListSize = 5000

// checkInList 校验变长 IN 列表的长度。
//
// 这是双引擎策略的差异点：SQLite 侧用 sqlc.slice()，在调用时展开成
// IN (?, ?, ?)；PostgreSQL 侧用 = ANY($n::text[]) 传原生数组。
// 两边 SQL 各写各的，repo 层把差异吸收掉，对上层暴露同一个 []string 签名。
// 防漂移靠 pkg/repo/parity_test.go 的跨引擎对照测试。
//
// SQLite 的展开会占用等量的绑定变量（上限 32766），因此长度必须设限；
// PostgreSQL 侧虽是单参数，也一并设限以免超大数组拖垮查询计划。
func checkInList(ids []string) error {
	if len(ids) > maxInListSize {
		return fmt.Errorf("单次最多处理 %d 个 ID，当前 %d 个", maxInListSize, len(ids))
	}
	return nil
}

func (s *Store) CreateMailAccount(ctx context.Context, a *model.MailAccount) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreateMailAccount(ctx, sqlitedb.CreateMailAccountParams{
			ID: a.ID, TenantID: a.TenantID, GroupID: a.GroupID,
			Email: a.Email, EmailNormalized: a.EmailNormalized,
			Provider: a.Provider, AccountType: a.AccountType,
			PasswordEnc: a.PasswordEnc, ClientID: a.ClientID, RefreshTokenEnc: a.RefreshTokenEnc,
			ImapHost: a.IMAPHost, ImapPort: int64(a.IMAPPort), ImapPasswordEnc: a.IMAPPasswordEnc,
			Status: string(a.Status), Remark: a.Remark, SortOrder: int64(a.SortOrder),
			ProxyUrl: a.ProxyURL, FallbackProxyUrl1: a.FallbackProxyURL1, FallbackProxyUrl2: a.FallbackProxyURL2,
		})
	} else {
		err = s.postgres.CreateMailAccount(ctx, postgresdb.CreateMailAccountParams{
			ID: a.ID, TenantID: a.TenantID, GroupID: a.GroupID,
			Email: a.Email, EmailNormalized: a.EmailNormalized,
			Provider: a.Provider, AccountType: a.AccountType,
			PasswordEnc: a.PasswordEnc, ClientID: a.ClientID, RefreshTokenEnc: a.RefreshTokenEnc,
			ImapHost: a.IMAPHost, ImapPort: int32(a.IMAPPort), ImapPasswordEnc: a.IMAPPasswordEnc,
			Status: string(a.Status), Remark: a.Remark, SortOrder: int32(a.SortOrder),
			ProxyUrl: a.ProxyURL, FallbackProxyUrl1: a.FallbackProxyURL1, FallbackProxyUrl2: a.FallbackProxyURL2,
		})
	}
	return normalize(err)
}

func (s *Store) GetMailAccount(ctx context.Context, tenantID, id string) (*model.MailAccount, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetMailAccount(ctx, sqlitedb.GetMailAccountParams{TenantID: tenantID, ID: id})
		if e != nil {
			return nil, normalize(e)
		}
		return mapSQLiteAccount(v), nil
	}
	v, e := s.postgres.GetMailAccount(ctx, postgresdb.GetMailAccountParams{TenantID: tenantID, ID: id})
	if e != nil {
		return nil, normalize(e)
	}
	return mapPostgresAccount(v), nil
}

func (s *Store) GetMailAccountByEmail(ctx context.Context, tenantID, emailNormalized string) (*model.MailAccount, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetMailAccountByEmail(ctx, sqlitedb.GetMailAccountByEmailParams{
			TenantID: tenantID, EmailNormalized: emailNormalized,
		})
		if e != nil {
			return nil, normalize(e)
		}
		return mapSQLiteAccount(v), nil
	}
	v, e := s.postgres.GetMailAccountByEmail(ctx, postgresdb.GetMailAccountByEmailParams{
		TenantID: tenantID, EmailNormalized: emailNormalized,
	})
	if e != nil {
		return nil, normalize(e)
	}
	return mapPostgresAccount(v), nil
}

func (s *Store) ListMailAccountsByIDs(ctx context.Context, tenantID string, ids []string) ([]model.MailAccount, error) {
	out := []model.MailAccount{}
	if len(ids) == 0 {
		return out, nil
	}
	if err := checkInList(ids); err != nil {
		return nil, err
	}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListMailAccountsByIDs(ctx, sqlitedb.ListMailAccountsByIDsParams{
			TenantID: tenantID, AccountIds: ids,
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, *mapSQLiteAccount(r))
		}
		return out, nil
	}
	rows, err := s.postgres.ListMailAccountsByIDs(ctx, postgresdb.ListMailAccountsByIDsParams{
		TenantID: tenantID, Column2: ids,
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, *mapPostgresAccount(r))
	}
	return out, nil
}

func (s *Store) CountMailAccounts(ctx context.Context, tenantID string) (int, error) {
	if s.driver == "sqlite" {
		n, e := s.sqlite.CountMailAccounts(ctx, tenantID)
		return int(n), e
	}
	n, e := s.postgres.CountMailAccounts(ctx, tenantID)
	return int(n), e
}

func (s *Store) UpdateMailAccount(ctx context.Context, a *model.MailAccount) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateMailAccount(ctx, sqlitedb.UpdateMailAccountParams{
			GroupID: a.GroupID, Provider: a.Provider, AccountType: a.AccountType,
			PasswordEnc: a.PasswordEnc, ClientID: a.ClientID, RefreshTokenEnc: a.RefreshTokenEnc,
			ImapHost: a.IMAPHost, ImapPort: int64(a.IMAPPort), ImapPasswordEnc: a.IMAPPasswordEnc,
			Status: string(a.Status), Remark: a.Remark,
			ProxyUrl: a.ProxyURL, FallbackProxyUrl1: a.FallbackProxyURL1, FallbackProxyUrl2: a.FallbackProxyURL2,
			TenantID: a.TenantID, ID: a.ID,
		})
	} else {
		n, e = s.postgres.UpdateMailAccount(ctx, postgresdb.UpdateMailAccountParams{
			GroupID: a.GroupID, Provider: a.Provider, AccountType: a.AccountType,
			PasswordEnc: a.PasswordEnc, ClientID: a.ClientID, RefreshTokenEnc: a.RefreshTokenEnc,
			ImapHost: a.IMAPHost, ImapPort: int32(a.IMAPPort), ImapPasswordEnc: a.IMAPPasswordEnc,
			Status: string(a.Status), Remark: a.Remark,
			ProxyUrl: a.ProxyURL, FallbackProxyUrl1: a.FallbackProxyURL1, FallbackProxyUrl2: a.FallbackProxyURL2,
			TenantID: a.TenantID, ID: a.ID,
		})
	}
	return rowsAffected(n, e)
}

// SoftDeleteMailAccount 软删除账号，并在同一条 SQL 里清空三个凭据密文列。
// 软删除只对「误删可恢复」有意义，凭据密文不该跟着 deleted_at 长期留存
// （恢复后需要用户重新填凭据）。
func (s *Store) SoftDeleteMailAccount(ctx context.Context, tenantID, id string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.SoftDeleteMailAccount(ctx, sqlitedb.SoftDeleteMailAccountParams{TenantID: tenantID, ID: id})
	} else {
		n, e = s.postgres.SoftDeleteMailAccount(ctx, postgresdb.SoftDeleteMailAccountParams{TenantID: tenantID, ID: id})
	}
	return rowsAffected(n, e)
}

// 以下批量方法返回实际影响的行数，供上层统计成功/失败数。
// 不存在或属于其它租户的 ID 不会被计入——WHERE 恒带 tenant_id。

func (s *Store) BatchMoveMailAccounts(ctx context.Context, tenantID, groupID string, ids []string) (int, error) {
	return s.batchExec(ctx, ids,
		func() (int64, error) {
			return s.sqlite.BatchMoveMailAccounts(ctx, sqlitedb.BatchMoveMailAccountsParams{
				GroupID: groupID, TenantID: tenantID, AccountIds: ids,
			})
		},
		func() (int64, error) {
			return s.postgres.BatchMoveMailAccounts(ctx, postgresdb.BatchMoveMailAccountsParams{
				GroupID: groupID, TenantID: tenantID, Column3: ids,
			})
		})
}

func (s *Store) BatchUpdateMailAccountStatus(ctx context.Context, tenantID, status string, ids []string) (int, error) {
	return s.batchExec(ctx, ids,
		func() (int64, error) {
			return s.sqlite.BatchUpdateMailAccountStatus(ctx, sqlitedb.BatchUpdateMailAccountStatusParams{
				Status: status, TenantID: tenantID, AccountIds: ids,
			})
		},
		func() (int64, error) {
			return s.postgres.BatchUpdateMailAccountStatus(ctx, postgresdb.BatchUpdateMailAccountStatusParams{
				Status: status, TenantID: tenantID, Column3: ids,
			})
		})
}

func (s *Store) BatchUpdateMailAccountProxy(ctx context.Context, tenantID, proxy, fallback1, fallback2 string, ids []string) (int, error) {
	return s.batchExec(ctx, ids,
		func() (int64, error) {
			return s.sqlite.BatchUpdateMailAccountProxy(ctx, sqlitedb.BatchUpdateMailAccountProxyParams{
				ProxyUrl: proxy, FallbackProxyUrl1: fallback1, FallbackProxyUrl2: fallback2,
				TenantID: tenantID, AccountIds: ids,
			})
		},
		func() (int64, error) {
			return s.postgres.BatchUpdateMailAccountProxy(ctx, postgresdb.BatchUpdateMailAccountProxyParams{
				ProxyUrl: proxy, FallbackProxyUrl1: fallback1, FallbackProxyUrl2: fallback2,
				TenantID: tenantID, Column5: ids,
			})
		})
}

func (s *Store) BatchSoftDeleteMailAccounts(ctx context.Context, tenantID string, ids []string) (int, error) {
	return s.batchExec(ctx, ids,
		func() (int64, error) {
			return s.sqlite.BatchSoftDeleteMailAccounts(ctx, sqlitedb.BatchSoftDeleteMailAccountsParams{
				TenantID: tenantID, AccountIds: ids,
			})
		},
		func() (int64, error) {
			return s.postgres.BatchSoftDeleteMailAccounts(ctx, postgresdb.BatchSoftDeleteMailAccountsParams{
				TenantID: tenantID, Column2: ids,
			})
		})
}

// batchExec 收敛「按驱动分派 + 编码 ID 列表 + 转换返回类型」这段在每个批量方法里
// 都一样的样板，两个闭包分别是 SQLite 与 PostgreSQL 的调用。
func (s *Store) batchExec(_ context.Context, ids []string, sqliteFn, postgresFn func() (int64, error)) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	if err := checkInList(ids); err != nil {
		return 0, err
	}
	if s.driver == "sqlite" {
		n, err := sqliteFn()
		return int(n), normalize(err)
	}
	n, err := postgresFn()
	return int(n), normalize(err)
}

func mapSQLiteAccount(a sqlitedb.MailAccount) *model.MailAccount {
	return &model.MailAccount{
		ID: a.ID, TenantID: a.TenantID, GroupID: a.GroupID,
		Email: a.Email, EmailNormalized: a.EmailNormalized,
		Provider: a.Provider, AccountType: a.AccountType, AuthChannel: a.AuthChannel,
		PasswordEnc: a.PasswordEnc, ClientID: a.ClientID, RefreshTokenEnc: a.RefreshTokenEnc,
		IMAPHost: a.ImapHost, IMAPPort: int(a.ImapPort), IMAPPasswordEnc: a.ImapPasswordEnc,
		Status: model.AccountStatus(a.Status), Remark: a.Remark, SortOrder: int(a.SortOrder),
		ProxyURL: a.ProxyUrl, FallbackProxyURL1: a.FallbackProxyUrl1, FallbackProxyURL2: a.FallbackProxyUrl2,
		LastRefreshAt:     timePtr(a.LastRefreshAt),
		LastRefreshStatus: model.RefreshStatus(a.LastRefreshStatus), LastRefreshError: a.LastRefreshError,
		LastRefreshErrorKind:  a.LastRefreshErrorKind,
		RefreshTokenUpdatedAt: timePtr(a.RefreshTokenUpdatedAt),
		CreatedAt:             a.CreatedAt, UpdatedAt: a.UpdatedAt, DeletedAt: timePtr(a.DeletedAt),
	}
}

func mapPostgresAccount(a postgresdb.MailAccount) *model.MailAccount {
	return &model.MailAccount{
		ID: a.ID, TenantID: a.TenantID, GroupID: a.GroupID,
		Email: a.Email, EmailNormalized: a.EmailNormalized,
		Provider: a.Provider, AccountType: a.AccountType, AuthChannel: a.AuthChannel,
		PasswordEnc: a.PasswordEnc, ClientID: a.ClientID, RefreshTokenEnc: a.RefreshTokenEnc,
		IMAPHost: a.ImapHost, IMAPPort: int(a.ImapPort), IMAPPasswordEnc: a.ImapPasswordEnc,
		Status: model.AccountStatus(a.Status), Remark: a.Remark, SortOrder: int(a.SortOrder),
		ProxyURL: a.ProxyUrl, FallbackProxyURL1: a.FallbackProxyUrl1, FallbackProxyURL2: a.FallbackProxyUrl2,
		LastRefreshAt:     timePtr(a.LastRefreshAt),
		LastRefreshStatus: model.RefreshStatus(a.LastRefreshStatus), LastRefreshError: a.LastRefreshError,
		LastRefreshErrorKind:  a.LastRefreshErrorKind,
		RefreshTokenUpdatedAt: timePtr(a.RefreshTokenUpdatedAt),
		CreatedAt:             a.CreatedAt, UpdatedAt: a.UpdatedAt, DeletedAt: timePtr(a.DeletedAt),
	}
}

// 以下三个是协议层的写回入口。都刻意做得很窄：它们在每次拉信/刷新时都会触发，
// 用 UpdateMailAccount 那种整行改写会把用户同时在编辑的字段覆盖掉。

// UpdateMailAccountAuthChannel 记下本次实际走通的通道，下次优先试它。
func (s *Store) UpdateMailAccountAuthChannel(ctx context.Context, tenantID, id, channel string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateMailAccountAuthChannel(ctx, sqlitedb.UpdateMailAccountAuthChannelParams{
			AuthChannel: channel, TenantID: tenantID, ID: id,
		})
	} else {
		n, e = s.postgres.UpdateMailAccountAuthChannel(ctx, postgresdb.UpdateMailAccountAuthChannelParams{
			AuthChannel: channel, TenantID: tenantID, ID: id,
		})
	}
	return rowsAffected(n, e)
}

// UpdateMailAccountRefreshToken 持久化微软轮换过的 refresh_token。
// 漏掉这一步，账号会在下一次刷新时失效。
func (s *Store) UpdateMailAccountRefreshToken(ctx context.Context, tenantID, id, tokenEnc string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateMailAccountRefreshToken(ctx, sqlitedb.UpdateMailAccountRefreshTokenParams{
			RefreshTokenEnc: tokenEnc, TenantID: tenantID, ID: id,
		})
	} else {
		n, e = s.postgres.UpdateMailAccountRefreshToken(ctx, postgresdb.UpdateMailAccountRefreshTokenParams{
			RefreshTokenEnc: tokenEnc, TenantID: tenantID, ID: id,
		})
	}
	return rowsAffected(n, e)
}

// UpdateMailAccountRefreshResult 记录最近一次访问的成败，供 Token 管理页聚合。
func (s *Store) UpdateMailAccountRefreshResult(ctx context.Context, tenantID, id, status, errMessage, errorKind string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateMailAccountRefreshResult(ctx, sqlitedb.UpdateMailAccountRefreshResultParams{
			LastRefreshStatus: status, LastRefreshError: errMessage, LastRefreshErrorKind: errorKind,
			TenantID: tenantID, ID: id,
		})
	} else {
		n, e = s.postgres.UpdateMailAccountRefreshResult(ctx, postgresdb.UpdateMailAccountRefreshResultParams{
			LastRefreshStatus: status, LastRefreshError: errMessage, LastRefreshErrorKind: errorKind,
			TenantID: tenantID, ID: id,
		})
	}
	return rowsAffected(n, e)
}

// UpdateMailAccountAuthorization 只替换已经验证通过的 OAuth 凭据和刷新状态。
// 账号的分组、备注、代理可能正在被用户编辑，重新授权绝不能整行覆盖。
func (s *Store) UpdateMailAccountAuthorization(ctx context.Context, tenantID, id, clientID, tokenEnc, channel string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateMailAccountAuthorization(ctx, sqlitedb.UpdateMailAccountAuthorizationParams{
			ClientID: clientID, RefreshTokenEnc: tokenEnc, AuthChannel: channel, TenantID: tenantID, ID: id,
		})
	} else {
		n, e = s.postgres.UpdateMailAccountAuthorization(ctx, postgresdb.UpdateMailAccountAuthorizationParams{
			ClientID: clientID, RefreshTokenEnc: tokenEnc, AuthChannel: channel, TenantID: tenantID, ID: id,
		})
	}
	return rowsAffected(n, e)
}
