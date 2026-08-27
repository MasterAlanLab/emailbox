package repo

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// sqlite 的可选筛选参数没有类型信息，sqlc 生成 interface{}；
// postgres 因为 SQL 里带了 ::text / ::int 转换，生成 sql.NullString / sql.NullInt32。
// 这两组结构体各自独立，因此下面按驱动各构造一次。
type sqliteFilterParams struct {
	TenantID                                    string
	Status, RefreshStatus, Provider, GroupID, Q interface{}
	RowLimit, RowOffset                         int64
}

type postgresFilterParams struct {
	TenantID                                    string
	Status, RefreshStatus, Provider, GroupID, Q sql.NullString
	RowLimit, RowOffset                         int32
}

// nullStr 把「空串表示不筛选」转换为 SQL 的 NULL。
func nullStr(v string) sql.NullString {
	if v == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: v, Valid: true}
}

// anyStr 是 nullStr 的 sqlite 版：直接给 nil 或值。
func anyStr(v string) interface{} {
	if v == "" {
		return nil
	}
	return v
}

// searchTerm 规范化搜索关键词：转小写以匹配入库时已小写的 email_normalized，
// 备注侧由 SQL 的 lower() 对齐。
//
// 搜索用的是字面子串匹配（SQLite 的 instr / PostgreSQL 的 strpos），不是 LIKE。
// 原因是 LIKE 需要 ESCAPE 子句才能让用户输入里的 % 和 _ 保持字面含义，
// 而 sqlc v1.30 的 SQLite 解析器无法解析 `LIKE sqlc.narg(q) ESCAPE '\\'`。
// 字面子串匹配对搜索框来说本来就更符合直觉，两个引擎的语义也完全一致。
func searchTerm(q string) string {
	return strings.ToLower(strings.TrimSpace(q))
}

// ListMailAccountsPage 按筛选条件分页取账号。
//
// 排序不能用绑定参数——sqlc 无法参数化 ORDER BY（sqlc.arg 会被原样留在 SQL 里，
// 裸 ? 则被静默丢弃，两者都要到运行时才炸）。因此每个 (排序字段, 方向) 组合
// 各有一条查询，这里按白名单分派。filter 已由 Normalize 收敛到合法取值，
// 因此 default 分支只会在代码写错时命中。
func (s *Store) ListMailAccountsPage(ctx context.Context, tenantID string, f model.AccountFilter) ([]model.MailAccount, error) {
	f.Normalize()
	key := f.Sort + "/" + f.Order
	out := []model.MailAccount{}

	if s.driver == "sqlite" {
		p := sqliteFilterParams{
			TenantID: tenantID,
			Status:   anyStr(f.Status), RefreshStatus: anyStr(f.RefreshStatus),
			Provider: anyStr(f.Provider), GroupID: anyStr(firstOrEmpty(f.GroupIDs)),
			Q:        anyStr(searchTerm(f.Query)),
			RowLimit: int64(f.Limit), RowOffset: int64(f.Offset()),
		}
		rows, err := s.listAccountsSQLite(ctx, key, p)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, *mapSQLiteAccount(r))
		}
		return out, nil
	}

	p := postgresFilterParams{
		TenantID: tenantID,
		Status:   nullStr(f.Status), RefreshStatus: nullStr(f.RefreshStatus),
		Provider: nullStr(f.Provider), GroupID: nullStr(firstOrEmpty(f.GroupIDs)),
		Q:        nullStr(searchTerm(f.Query)),
		RowLimit: int32(f.Limit), RowOffset: int32(f.Offset()),
	}
	rows, err := s.listAccountsPostgres(ctx, key, p)
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		out = append(out, *mapPostgresAccount(r))
	}
	return out, nil
}

func (s *Store) listAccountsSQLite(ctx context.Context, key string, p sqliteFilterParams) ([]sqlitedb.MailAccount, error) {
	switch key {
	case "sort_order/asc":
		rows, err := s.sqlite.ListMailAccountsPageBySortOrderAsc(ctx, sqlitedb.ListMailAccountsPageBySortOrderAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "sort_order/desc":
		rows, err := s.sqlite.ListMailAccountsPageBySortOrderDesc(ctx, sqlitedb.ListMailAccountsPageBySortOrderDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "email/asc":
		rows, err := s.sqlite.ListMailAccountsPageByEmailAsc(ctx, sqlitedb.ListMailAccountsPageByEmailAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "email/desc":
		rows, err := s.sqlite.ListMailAccountsPageByEmailDesc(ctx, sqlitedb.ListMailAccountsPageByEmailDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "created_at/asc":
		rows, err := s.sqlite.ListMailAccountsPageByCreatedAtAsc(ctx, sqlitedb.ListMailAccountsPageByCreatedAtAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "created_at/desc":
		rows, err := s.sqlite.ListMailAccountsPageByCreatedAtDesc(ctx, sqlitedb.ListMailAccountsPageByCreatedAtDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "last_refresh_at/asc":
		rows, err := s.sqlite.ListMailAccountsPageByLastRefreshAtAsc(ctx, sqlitedb.ListMailAccountsPageByLastRefreshAtAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "last_refresh_at/desc":
		rows, err := s.sqlite.ListMailAccountsPageByLastRefreshAtDesc(ctx, sqlitedb.ListMailAccountsPageByLastRefreshAtDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	default:
		return nil, fmt.Errorf("不支持的排序组合 %q", key)
	}
}

func (s *Store) listAccountsPostgres(ctx context.Context, key string, p postgresFilterParams) ([]postgresdb.MailAccount, error) {
	switch key {
	case "sort_order/asc":
		rows, err := s.postgres.ListMailAccountsPageBySortOrderAsc(ctx, postgresdb.ListMailAccountsPageBySortOrderAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "sort_order/desc":
		rows, err := s.postgres.ListMailAccountsPageBySortOrderDesc(ctx, postgresdb.ListMailAccountsPageBySortOrderDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "email/asc":
		rows, err := s.postgres.ListMailAccountsPageByEmailAsc(ctx, postgresdb.ListMailAccountsPageByEmailAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "email/desc":
		rows, err := s.postgres.ListMailAccountsPageByEmailDesc(ctx, postgresdb.ListMailAccountsPageByEmailDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "created_at/asc":
		rows, err := s.postgres.ListMailAccountsPageByCreatedAtAsc(ctx, postgresdb.ListMailAccountsPageByCreatedAtAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "created_at/desc":
		rows, err := s.postgres.ListMailAccountsPageByCreatedAtDesc(ctx, postgresdb.ListMailAccountsPageByCreatedAtDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "last_refresh_at/asc":
		rows, err := s.postgres.ListMailAccountsPageByLastRefreshAtAsc(ctx, postgresdb.ListMailAccountsPageByLastRefreshAtAscParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	case "last_refresh_at/desc":
		rows, err := s.postgres.ListMailAccountsPageByLastRefreshAtDesc(ctx, postgresdb.ListMailAccountsPageByLastRefreshAtDescParams{
			TenantID: p.TenantID, Status: p.Status, RefreshStatus: p.RefreshStatus,
			Provider: p.Provider, GroupID: p.GroupID,
			Q: p.Q, RowLimit: p.RowLimit, RowOffset: p.RowOffset,
		})
		return rows, err
	default:
		return nil, fmt.Errorf("不支持的排序组合 %q", key)
	}
}

// CountMailAccountsFiltered 返回与 ListMailAccountsPage 相同筛选条件下的总数。
func (s *Store) CountMailAccountsFiltered(ctx context.Context, tenantID string, f model.AccountFilter) (int, error) {
	f.Normalize()
	if s.driver == "sqlite" {
		arg := sqlitedb.CountMailAccountsFilteredParams{
			TenantID: tenantID,
			Status:   anyStr(f.Status), RefreshStatus: anyStr(f.RefreshStatus),
			Provider: anyStr(f.Provider), GroupID: anyStr(firstOrEmpty(f.GroupIDs)),
			Q: anyStr(searchTerm(f.Query)),
		}
		n, err := s.sqlite.CountMailAccountsFiltered(ctx, arg)
		return int(n), err
	}
	arg := postgresdb.CountMailAccountsFilteredParams{
		TenantID: tenantID,
		Status:   nullStr(f.Status), RefreshStatus: nullStr(f.RefreshStatus),
		Provider: nullStr(f.Provider), GroupID: nullStr(firstOrEmpty(f.GroupIDs)),
		Q: nullStr(searchTerm(f.Query)),
	}
	n, err := s.postgres.CountMailAccountsFiltered(ctx, arg)
	return int(n), err
}

// firstOrEmpty 取切片首元素。当前筛选只支持单个分组
// （多分组的场景由 service 展开成多次查询）。
func firstOrEmpty(v []string) string {
	if len(v) == 0 {
		return ""
	}
	return v[0]
}
