package repo

import (
	"context"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

func (s *Store) CreateMailGroup(ctx context.Context, g *model.MailGroup) error {
	var err error
	if s.driver == "sqlite" {
		err = s.sqlite.CreateMailGroup(ctx, sqlitedb.CreateMailGroupParams{
			ID: g.ID, TenantID: g.TenantID,
			Name: g.Name, Description: g.Description, Color: string(g.Color),
			SortOrder: int64(g.SortOrder), IsSystem: boolToInt64(g.IsSystem),
			ProxyUrl: g.ProxyURL, FallbackProxyUrl1: g.FallbackProxyURL1, FallbackProxyUrl2: g.FallbackProxyURL2,
		})
	} else {
		err = s.postgres.CreateMailGroup(ctx, postgresdb.CreateMailGroupParams{
			ID: g.ID, TenantID: g.TenantID,
			Name: g.Name, Description: g.Description, Color: string(g.Color),
			SortOrder: int32(g.SortOrder), IsSystem: boolToInt32(g.IsSystem),
			ProxyUrl: g.ProxyURL, FallbackProxyUrl1: g.FallbackProxyURL1, FallbackProxyUrl2: g.FallbackProxyURL2,
		})
	}
	return normalize(err)
}

func (s *Store) GetMailGroup(ctx context.Context, tenantID, id string) (*model.MailGroup, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetMailGroup(ctx, sqlitedb.GetMailGroupParams{TenantID: tenantID, ID: id})
		if e != nil {
			return nil, normalize(e)
		}
		return mapSQLiteGroup(v), nil
	}
	v, e := s.postgres.GetMailGroup(ctx, postgresdb.GetMailGroupParams{TenantID: tenantID, ID: id})
	if e != nil {
		return nil, normalize(e)
	}
	return mapPostgresGroup(v), nil
}

// GetSystemMailGroup 返回租户的系统默认分组。删除其它分组时账号回落到它，
// 因此它必须始终存在——注册事务里创建，且 DeleteMailGroup 拒绝删除 is_system=1。
func (s *Store) GetSystemMailGroup(ctx context.Context, tenantID string) (*model.MailGroup, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetSystemMailGroup(ctx, tenantID)
		if e != nil {
			return nil, normalize(e)
		}
		return mapSQLiteGroup(v), nil
	}
	v, e := s.postgres.GetSystemMailGroup(ctx, tenantID)
	if e != nil {
		return nil, normalize(e)
	}
	return mapPostgresGroup(v), nil
}

func (s *Store) ListMailGroups(ctx context.Context, tenantID string) ([]model.MailGroup, error) {
	out := []model.MailGroup{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListMailGroups(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, *mapSQLiteGroup(r))
		}
		return out, nil
	}
	rows, err := s.postgres.ListMailGroups(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, *mapPostgresGroup(r))
	}
	return out, nil
}

func (s *Store) CountMailGroups(ctx context.Context, tenantID string) (int, error) {
	if s.driver == "sqlite" {
		n, e := s.sqlite.CountMailGroups(ctx, tenantID)
		return int(n), e
	}
	n, e := s.postgres.CountMailGroups(ctx, tenantID)
	return int(n), e
}

func (s *Store) UpdateMailGroup(ctx context.Context, g *model.MailGroup) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateMailGroup(ctx, sqlitedb.UpdateMailGroupParams{
			Name: g.Name, Description: g.Description, Color: string(g.Color),
			ProxyUrl: g.ProxyURL, FallbackProxyUrl1: g.FallbackProxyURL1, FallbackProxyUrl2: g.FallbackProxyURL2,
			TenantID: g.TenantID, ID: g.ID,
		})
	} else {
		n, e = s.postgres.UpdateMailGroup(ctx, postgresdb.UpdateMailGroupParams{
			Name: g.Name, Description: g.Description, Color: string(g.Color),
			ProxyUrl: g.ProxyURL, FallbackProxyUrl1: g.FallbackProxyURL1, FallbackProxyUrl2: g.FallbackProxyURL2,
			TenantID: g.TenantID, ID: g.ID,
		})
	}
	return rowsAffected(n, e)
}

func (s *Store) UpdateMailGroupSort(ctx context.Context, tenantID, id string, sortOrder int) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.UpdateMailGroupSort(ctx, sqlitedb.UpdateMailGroupSortParams{
			SortOrder: int64(sortOrder), TenantID: tenantID, ID: id,
		}))
	}
	return normalize(s.postgres.UpdateMailGroupSort(ctx, postgresdb.UpdateMailGroupSortParams{
		SortOrder: int32(sortOrder), TenantID: tenantID, ID: id,
	}))
}

// DeleteMailGroup 物理删除分组。
// 系统分组删不掉（SQL 里带 is_system = 0），返回 ErrNotFound。
func (s *Store) DeleteMailGroup(ctx context.Context, tenantID, id string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.DeleteMailGroup(ctx, sqlitedb.DeleteMailGroupParams{TenantID: tenantID, ID: id})
	} else {
		n, e = s.postgres.DeleteMailGroup(ctx, postgresdb.DeleteMailGroupParams{TenantID: tenantID, ID: id})
	}
	return rowsAffected(n, e)
}

// CountAccountsPerGroup 一次取回租户下各分组的账号数，避免按分组逐个 COUNT 的 N+1。
func (s *Store) CountAccountsPerGroup(ctx context.Context, tenantID string) (map[string]int, error) {
	counts := map[string]int{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.CountAccountsPerGroup(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			counts[r.GroupID] = int(r.AccountCount)
		}
		return counts, nil
	}
	rows, err := s.postgres.CountAccountsPerGroup(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		counts[r.GroupID] = int(r.AccountCount)
	}
	return counts, nil
}

// MoveAccountsToGroup 把 fromGroupID 下的账号整体移到 toGroupID，
// 用于删除分组时让账号回落到默认分组而不是被级联删掉。
func (s *Store) MoveAccountsToGroup(ctx context.Context, tenantID, fromGroupID, toGroupID string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.MoveAccountsToGroup(ctx, sqlitedb.MoveAccountsToGroupParams{
			GroupID: toGroupID, TenantID: tenantID, GroupID_2: fromGroupID,
		}))
	}
	return normalize(s.postgres.MoveAccountsToGroup(ctx, postgresdb.MoveAccountsToGroupParams{
		GroupID: toGroupID, TenantID: tenantID, GroupID_2: fromGroupID,
	}))
}

func mapSQLiteGroup(g sqlitedb.MailGroup) *model.MailGroup {
	return &model.MailGroup{
		ID: g.ID, TenantID: g.TenantID,
		Name: g.Name, Description: g.Description, Color: model.GroupColor(g.Color),
		SortOrder: int(g.SortOrder), IsSystem: g.IsSystem != 0,
		ProxyURL: g.ProxyUrl, FallbackProxyURL1: g.FallbackProxyUrl1, FallbackProxyURL2: g.FallbackProxyUrl2,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
}

func mapPostgresGroup(g postgresdb.MailGroup) *model.MailGroup {
	return &model.MailGroup{
		ID: g.ID, TenantID: g.TenantID,
		Name: g.Name, Description: g.Description, Color: model.GroupColor(g.Color),
		SortOrder: int(g.SortOrder), IsSystem: g.IsSystem != 0,
		ProxyURL: g.ProxyUrl, FallbackProxyURL1: g.FallbackProxyUrl1, FallbackProxyURL2: g.FallbackProxyUrl2,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
	}
}
