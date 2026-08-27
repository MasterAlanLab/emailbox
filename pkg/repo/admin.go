package repo

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strconv"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// asInt64 收拢聚合函数（SUM / MAX）的返回值。
//
// 这类列的静态类型五花八门：sqlc 对 SQLite 的 COUNT 给 int64、对 SUM 给
// sql.NullFloat64、对 MAX 给 interface{}；PostgreSQL 那边 SUM(integer) 是
// numeric，驱动可能回 int64、float64 或 []byte。全都要接住。
//
// 兜底分支必须出声。这里原本是个安静的 return 0，结果 sql.NullFloat64
// 走进去之后，刷新统计的成功/失败数一律显示成 0——页面看着正常，
// 数字全是错的，没人会发现。统计值宁可报警也不能静默归零。
func asInt64(v any) int64 {
	switch n := v.(type) {
	case nil:
		return 0
	case int64:
		return n
	case int32:
		return int64(n)
	case float64:
		return int64(n)
	case sql.NullInt64:
		return n.Int64
	case sql.NullInt32:
		return int64(n.Int32)
	case sql.NullFloat64:
		return int64(n.Float64)
	case []byte:
		return parseInt64(string(n))
	case string:
		return parseInt64(n)
	default:
		slog.Error("聚合结果的类型不认识，按 0 处理（统计会偏低）",
			"type", fmt.Sprintf("%T", v))
		return 0
	}
}

func parseInt64(s string) int64 {
	parsed, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		slog.Error("聚合结果解析失败，按 0 处理", "value", s, "error", err)
		return 0
	}
	return parsed
}

// ListAdminUsers 按条件分页取用户，同时返回总条数。
func (s *Store) ListAdminUsers(ctx context.Context, f model.AdminUserFilter) ([]model.AdminUser, int, error) {
	f.Normalize()
	q := searchTerm(f.Query)
	out := []model.AdminUser{}

	if s.driver == "sqlite" {
		rows, err := s.sqlite.ListAdminUsersPage(ctx, sqlitedb.ListAdminUsersPageParams{
			Status:       anyStr(f.Status),
			PlatformRole: anyStr(f.PlatformRole),
			Q:            anyStr(q),
			RowLimit:     int64(f.Limit),
			RowOffset:    int64(f.Offset()),
		})
		if err != nil {
			return nil, 0, err
		}
		for _, r := range rows {
			out = append(out, model.AdminUser{
				ID: r.ID, Username: r.Username, Email: r.Email,
				Status: model.UserStatus(r.Status), PlatformRole: model.PlatformRole(r.PlatformRole),
				CreatedAt: r.CreatedAt, LastLoginAt: timePtr(r.LastLoginAt),
				TenantID: r.TenantID, TenantName: r.TenantName, AccountCount: int(r.AccountCount),
				PlanCode: r.PlanCode, MaxAccounts: int(r.MaxAccounts),
			})
			out[len(out)-1].ComputeOverQuota()
		}
		total, err := s.sqlite.CountAdminUsers(ctx, sqlitedb.CountAdminUsersParams{
			Status:       anyStr(f.Status),
			PlatformRole: anyStr(f.PlatformRole),
			Q:            anyStr(q),
		})
		return out, int(total), err
	}

	rows, err := s.postgres.ListAdminUsersPage(ctx, postgresdb.ListAdminUsersPageParams{
		Status:       nullStr(f.Status),
		PlatformRole: nullStr(f.PlatformRole),
		Q:            nullStr(q),
		RowLimit:     int32(f.Limit),
		RowOffset:    int32(f.Offset()),
	})
	if err != nil {
		return nil, 0, err
	}
	for _, r := range rows {
		out = append(out, model.AdminUser{
			ID: r.ID, Username: r.Username, Email: r.Email,
			Status: model.UserStatus(r.Status), PlatformRole: model.PlatformRole(r.PlatformRole),
			CreatedAt: r.CreatedAt, LastLoginAt: timePtr(r.LastLoginAt),
			TenantID: r.TenantID, TenantName: r.TenantName, AccountCount: int(r.AccountCount),
			PlanCode: r.PlanCode, MaxAccounts: int(r.MaxAccounts),
		})
		out[len(out)-1].ComputeOverQuota()
	}
	total, err := s.postgres.CountAdminUsers(ctx, postgresdb.CountAdminUsersParams{
		Status:       nullStr(f.Status),
		PlatformRole: nullStr(f.PlatformRole),
		Q:            nullStr(q),
	})
	return out, int(total), err
}

// GetAdminUser 取单个用户的后台视图（含个人空间与邮箱数）。
func (s *Store) GetAdminUser(ctx context.Context, userID string) (*model.AdminUser, error) {
	if s.driver == "sqlite" {
		r, err := s.sqlite.GetAdminUser(ctx, userID)
		if err != nil {
			return nil, normalize(err)
		}
		u := &model.AdminUser{
			ID: r.ID, Username: r.Username, Email: r.Email,
			Status: model.UserStatus(r.Status), PlatformRole: model.PlatformRole(r.PlatformRole),
			CreatedAt: r.CreatedAt, LastLoginAt: timePtr(r.LastLoginAt),
			TenantID: r.TenantID, TenantName: r.TenantName, AccountCount: int(r.AccountCount),
			PlanCode: r.PlanCode, MaxAccounts: int(r.MaxAccounts),
		}
		u.ComputeOverQuota()
		return u, nil
	}
	r, err := s.postgres.GetAdminUser(ctx, userID)
	if err != nil {
		return nil, normalize(err)
	}
	u := &model.AdminUser{
		ID: r.ID, Username: r.Username, Email: r.Email,
		Status: model.UserStatus(r.Status), PlatformRole: model.PlatformRole(r.PlatformRole),
		CreatedAt: r.CreatedAt, LastLoginAt: timePtr(r.LastLoginAt),
		TenantID: r.TenantID, TenantName: r.TenantName, AccountCount: int(r.AccountCount),
		PlanCode: r.PlanCode, MaxAccounts: int(r.MaxAccounts),
	}
	u.ComputeOverQuota()
	return u, nil
}

// TouchUserLastLogin 记录一次成功登录的时间。
// 失败不影响登录本身，调用方按「记不上就算了」处理。
func (s *Store) TouchUserLastLogin(ctx context.Context, userID string) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.UpdateUserLastLogin(ctx, userID))
	}
	return normalize(s.postgres.UpdateUserLastLogin(ctx, userID))
}

// SoftDeleteUser 软删用户本体。级联清理（个人空间、邮箱账号、会话）由 service 编排。
func (s *Store) SoftDeleteUser(ctx context.Context, userID string) error {
	if s.driver == "sqlite" {
		return rowsAffected(s.sqlite.SoftDeleteUser(ctx, userID))
	}
	return rowsAffected(s.postgres.SoftDeleteUser(ctx, userID))
}

// GetPersonalTenantByOwner 取用户的个人工作空间。
func (s *Store) GetPersonalTenantByOwner(ctx context.Context, userID string) (*model.Tenant, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetPersonalTenantByOwner(ctx, userID)
		x := mapSTenant(v)
		return &x, normalize(e)
	}
	v, e := s.postgres.GetPersonalTenantByOwner(ctx, userID)
	x := mapPTenant(v)
	return &x, normalize(e)
}

// SoftDeleteMailAccountsByTenant 软删整个租户的邮箱，并在同一条语句里清空凭据密文。
// 返回受影响行数，供审计记录「删了多少个」。
func (s *Store) SoftDeleteMailAccountsByTenant(ctx context.Context, tenantID string) (int, error) {
	var (
		n   int64
		err error
	)
	if s.driver == "sqlite" {
		n, err = s.sqlite.SoftDeleteMailAccountsByTenant(ctx, tenantID)
	} else {
		n, err = s.postgres.SoftDeleteMailAccountsByTenant(ctx, tenantID)
	}
	if err != nil {
		return 0, normalize(err)
	}
	return int(n), nil
}

// GetPlatformStats 一次取齐总览卡片的所有计数。day 是租户时区下的 YYYY-MM-DD。
func (s *Store) GetPlatformStats(ctx context.Context, day string) (*model.PlatformStats, error) {
	if s.driver == "sqlite" {
		r, err := s.sqlite.GetPlatformStats(ctx, day)
		if err != nil {
			return nil, normalize(err)
		}
		return &model.PlatformStats{
			UserCount: int(r.UserCount), DisabledUserCount: int(r.DisabledUserCount),
			AdminCount: int(r.AdminCount), TenantCount: int(r.TenantCount),
			AccountCount: int(r.AccountCount), BannedAccountCount: int(r.BannedAccountCount),
			MailFetchToday:    int(asInt64(r.MailFetchToday)),
			TokenRefreshToday: int(asInt64(r.TokenRefreshToday)),
		}, nil
	}
	r, err := s.postgres.GetPlatformStats(ctx, day)
	if err != nil {
		return nil, normalize(err)
	}
	return &model.PlatformStats{
		UserCount: int(r.UserCount), DisabledUserCount: int(r.DisabledUserCount),
		AdminCount: int(r.AdminCount), TenantCount: int(r.TenantCount),
		AccountCount: int(r.AccountCount), BannedAccountCount: int(r.BannedAccountCount),
		MailFetchToday:    int(asInt64(r.MailFetchToday)),
		TokenRefreshToday: int(asInt64(r.TokenRefreshToday)),
	}, nil
}
