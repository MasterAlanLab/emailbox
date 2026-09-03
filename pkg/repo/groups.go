package repo

import (
	"context"
	"time"

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
			ProxyUrl: g.ProxyURL,
		})
	} else {
		err = s.postgres.CreateMailGroup(ctx, postgresdb.CreateMailGroupParams{
			ID: g.ID, TenantID: g.TenantID,
			Name: g.Name, Description: g.Description, Color: string(g.Color),
			SortOrder: int32(g.SortOrder), IsSystem: boolToInt32(g.IsSystem),
			ProxyUrl: g.ProxyURL,
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
			ProxyUrl: g.ProxyURL,
			TenantID: g.TenantID, ID: g.ID,
		})
	} else {
		n, e = s.postgres.UpdateMailGroup(ctx, postgresdb.UpdateMailGroupParams{
			Name: g.Name, Description: g.Description, Color: string(g.Color),
			ProxyUrl: g.ProxyURL,
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

// UpdateMailGroupSchedule 只改定时刷新的两列，是一条**窄** UPDATE。
//
// 不并进 UpdateMailGroup 的理由和协议层写回令牌是同一个（AGENTS.md §5.4）：
// 调度器每个周期都会推进 next_refresh_at，用整行改写会把用户此刻正开着编辑框
// 改到一半的名称、描述、代理一起盖掉。
func (s *Store) UpdateMailGroupSchedule(
	ctx context.Context, tenantID, id string, intervalMinutes int, nextRefreshAt *time.Time,
) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateMailGroupSchedule(ctx, sqlitedb.UpdateMailGroupScheduleParams{
			RefreshIntervalMinutes: int64(intervalMinutes),
			NextRefreshAt:          nullableTime(nextRefreshAt),
			TenantID:               tenantID, ID: id,
		})
	} else {
		n, e = s.postgres.UpdateMailGroupSchedule(ctx, postgresdb.UpdateMailGroupScheduleParams{
			RefreshIntervalMinutes: int32(intervalMinutes),
			NextRefreshAt:          nullableTime(nextRefreshAt),
			TenantID:               tenantID, ID: id,
		})
	}
	return rowsAffected(n, e)
}

// ListGroupsDueForRefresh 返回到期该刷新的分组，**跨全部租户**。
//
// 没有 tenant_id 是有意的：它和 ListStaleJobs、DeleteExpiredSessions 一样属于
// 运维查询，调用方是进程内的后台 goroutine 而不是某个租户的请求。租户边界在
// 下游守住——调度器只会用行上自带的 TenantID 去建任务，不接受外部传入的租户。
func (s *Store) ListGroupsDueForRefresh(
	ctx context.Context, dueBefore time.Time, limit int,
) ([]model.MailGroup, error) {
	at := utcNullTime(dueBefore)
	out := []model.MailGroup{}
	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListGroupsDueForRefresh(ctx, sqlitedb.ListGroupsDueForRefreshParams{
			DueBefore: at, RowLimit: int64(limit),
		})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, *mapSQLiteGroup(r))
		}
		return out, nil
	}
	rows, err := s.postgres.ListGroupsDueForRefresh(ctx, postgresdb.ListGroupsDueForRefreshParams{
		DueBefore: at, RowLimit: int32(limit),
	})
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, *mapPostgresGroup(r))
	}
	return out, nil
}

// ClaimGroupRefresh 把一个到期分组的 next_refresh_at 推到 nextRefreshAt，
// 抢到了返回 true。抢不到（false）说明这一轮不该由本次调用来处理它。
//
// 三种抢不到的情况都是对的：另一个实例先手了、用户在扫描和抢占之间关掉了定时、
// 用户改了间隔（改间隔会立刻重算 next_refresh_at 到未来）。
//
// 它有意不碰 updated_at：那一列的含义是「用户改过这个分组」，
// 而调度器推进周期不是用户的改动，跟着动会让每个分组看起来每小时都被编辑过。
func (s *Store) ClaimGroupRefresh(
	ctx context.Context, id string, dueBefore, nextRefreshAt time.Time,
) (bool, error) {
	var n int64
	var e error
	due := utcNullTime(dueBefore)
	next := utcNullTime(nextRefreshAt)
	if s.driver == "sqlite" {
		n, e = s.sqlite.ClaimGroupRefresh(ctx, sqlitedb.ClaimGroupRefreshParams{
			NextRefreshAt: next, ID: id, DueBefore: due,
		})
	} else {
		n, e = s.postgres.ClaimGroupRefresh(ctx, postgresdb.ClaimGroupRefreshParams{
			NextRefreshAt: next, ID: id, DueBefore: due,
		})
	}
	if e != nil {
		return false, normalize(e)
	}
	return n > 0, nil
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
		ProxyURL:  g.ProxyUrl,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
		RefreshIntervalMinutes: int(g.RefreshIntervalMinutes),
		NextRefreshAt:          timePtr(g.NextRefreshAt),
	}
}

func mapPostgresGroup(g postgresdb.MailGroup) *model.MailGroup {
	return &model.MailGroup{
		ID: g.ID, TenantID: g.TenantID,
		Name: g.Name, Description: g.Description, Color: model.GroupColor(g.Color),
		SortOrder: int(g.SortOrder), IsSystem: g.IsSystem != 0,
		ProxyURL:  g.ProxyUrl,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
		RefreshIntervalMinutes: int(g.RefreshIntervalMinutes),
		NextRefreshAt:          timePtr(g.NextRefreshAt),
	}
}
