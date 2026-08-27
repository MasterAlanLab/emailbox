package repo

import (
	"context"
	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

func (s *Store) CreateMember(ctx context.Context, m *model.TenantMember) error {
	var e error
	if s.driver == "sqlite" {
		e = s.sqlite.CreateTenantMember(ctx, sqlitedb.CreateTenantMemberParams{ID: m.ID, TenantID: m.TenantID, UserID: m.UserID, Role: string(m.Role)})
	} else {
		e = s.postgres.CreateTenantMember(ctx, postgresdb.CreateTenantMemberParams{ID: m.ID, TenantID: m.TenantID, UserID: m.UserID, Role: string(m.Role)})
	}
	return normalize(e)
}
func (s *Store) GetMember(ctx context.Context, tenantID, userID string) (*model.TenantMember, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetTenantMember(ctx, sqlitedb.GetTenantMemberParams{TenantID: tenantID, UserID: userID})
		return &model.TenantMember{ID: v.ID, TenantID: v.TenantID, UserID: v.UserID, Role: model.TenantRole(v.Role), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}, normalize(e)
	}
	v, e := s.postgres.GetTenantMember(ctx, postgresdb.GetTenantMemberParams{TenantID: tenantID, UserID: userID})
	return &model.TenantMember{ID: v.ID, TenantID: v.TenantID, UserID: v.UserID, Role: model.TenantRole(v.Role), CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}, normalize(e)
}
func (s *Store) ListMembers(ctx context.Context, tenantID string) ([]model.TenantMemberDetail, error) {
	out := []model.TenantMemberDetail{}
	if s.driver == "sqlite" {
		v, e := s.sqlite.ListTenantMembers(ctx, tenantID)
		if e != nil {
			return nil, e
		}
		for _, x := range v {
			out = append(out, model.TenantMemberDetail{TenantMember: model.TenantMember{ID: x.ID, TenantID: x.TenantID, UserID: x.UserID, Role: model.TenantRole(x.Role), CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt}, Username: x.Username, Email: x.Email})
		}
	} else {
		v, e := s.postgres.ListTenantMembers(ctx, tenantID)
		if e != nil {
			return nil, e
		}
		for _, x := range v {
			out = append(out, model.TenantMemberDetail{TenantMember: model.TenantMember{ID: x.ID, TenantID: x.TenantID, UserID: x.UserID, Role: model.TenantRole(x.Role), CreatedAt: x.CreatedAt, UpdatedAt: x.UpdatedAt}, Username: x.Username, Email: x.Email})
		}
	}
	return out, nil
}
func (s *Store) UpdateMemberRole(ctx context.Context, tenantID, userID string, role model.TenantRole) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateTenantMemberRole(ctx, sqlitedb.UpdateTenantMemberRoleParams{Role: string(role), TenantID: tenantID, UserID: userID})
	} else {
		n, e = s.postgres.UpdateTenantMemberRole(ctx, postgresdb.UpdateTenantMemberRoleParams{Role: string(role), TenantID: tenantID, UserID: userID})
	}
	if e != nil {
		return e
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteMemberKeepingOwner 删除成员，但拒绝删除租户的最后一个 owner。
// 判断和删除在同一条 SQL 里完成，因此并发删除不会把租户变成无人可管理的状态；
// 先查询再删除的写法在两个请求交错时会双双通过检查。
// 返回 ErrConflict 表示该成员是最后一个 owner。
func (s *Store) DeleteMemberKeepingOwner(ctx context.Context, tenantID, userID string) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		// SQLite 的 ? 是位置参数，租户 ID 在外层和子查询各出现一次。
		n, e = s.sqlite.DeleteTenantMemberKeepingOwner(ctx, sqlitedb.DeleteTenantMemberKeepingOwnerParams{
			TenantID: tenantID, UserID: userID, TenantID_2: tenantID})
	} else {
		// PostgreSQL 的 $1 可复用，因此只有两个参数。
		n, e = s.postgres.DeleteTenantMemberKeepingOwner(ctx, postgresdb.DeleteTenantMemberKeepingOwnerParams{
			TenantID: tenantID, UserID: userID})
	}
	if e != nil {
		return e
	}
	if n == 0 {
		return ErrConflict
	}
	return nil
}

func (s *Store) CountOwners(ctx context.Context, tenantID string) (int64, error) {
	if s.driver == "sqlite" {
		return s.sqlite.CountTenantOwners(ctx, tenantID)
	}
	return s.postgres.CountTenantOwners(ctx, tenantID)
}
