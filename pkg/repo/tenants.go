package repo

import (
	"context"
	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

func (s *Store) CreateTenant(ctx context.Context, t *model.Tenant) error {
	var e error
	if s.driver == "sqlite" {
		e = s.sqlite.CreateTenant(ctx, sqlitedb.CreateTenantParams{ID: t.ID, Name: t.Name, Slug: t.Slug, CreatedBy: t.CreatedBy, Kind: string(t.Kind)})
	} else {
		e = s.postgres.CreateTenant(ctx, postgresdb.CreateTenantParams{ID: t.ID, Name: t.Name, Slug: t.Slug, CreatedBy: t.CreatedBy, Kind: string(t.Kind)})
	}
	return normalize(e)
}
func mapSTenant(t sqlitedb.Tenant) model.Tenant {
	return model.Tenant{ID: t.ID, Name: t.Name, Slug: t.Slug, Kind: model.TenantKind(t.Kind), CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, DeletedAt: timePtr(t.DeletedAt)}
}
func mapPTenant(t postgresdb.Tenant) model.Tenant {
	return model.Tenant{ID: t.ID, Name: t.Name, Slug: t.Slug, Kind: model.TenantKind(t.Kind), CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, UpdatedAt: t.UpdatedAt, DeletedAt: timePtr(t.DeletedAt)}
}
func (s *Store) GetTenantByID(ctx context.Context, id string) (*model.Tenant, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetTenantByID(ctx, id)
		x := mapSTenant(v)
		return &x, normalize(e)
	}
	v, e := s.postgres.GetTenantByID(ctx, id)
	x := mapPTenant(v)
	return &x, normalize(e)
}
func (s *Store) ListTenants(ctx context.Context, userID string) ([]model.Tenant, error) {
	out := []model.Tenant{}
	if s.driver == "sqlite" {
		v, e := s.sqlite.ListTenantsByUserID(ctx, userID)
		if e != nil {
			return nil, e
		}
		for _, x := range v {
			out = append(out, mapSTenant(x))
		}
	} else {
		v, e := s.postgres.ListTenantsByUserID(ctx, userID)
		if e != nil {
			return nil, e
		}
		for _, x := range v {
			out = append(out, mapPTenant(x))
		}
	}
	return out, nil
}
func (s *Store) UpdateTenant(ctx context.Context, t *model.Tenant) error {
	var n int64
	var e error
	if s.driver == "sqlite" {
		n, e = s.sqlite.UpdateTenant(ctx, sqlitedb.UpdateTenantParams{Name: t.Name, Slug: t.Slug, ID: t.ID})
	} else {
		n, e = s.postgres.UpdateTenant(ctx, postgresdb.UpdateTenantParams{Name: t.Name, Slug: t.Slug, ID: t.ID})
	}
	if e != nil {
		return normalize(e)
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
func (s *Store) DeleteTenant(ctx context.Context, id string) error {
	// 软删除和清理会话必须同一个事务：否则中途失败会留下指向已删除租户的会话。
	return s.WithTx(ctx, func(tx *Store) error {
		var n int64
		var e error
		if tx.driver == "sqlite" {
			n, e = tx.sqlite.SoftDeleteTenant(ctx, id)
		} else {
			n, e = tx.postgres.SoftDeleteTenant(ctx, id)
		}
		if e != nil {
			return e
		}
		if n == 0 {
			return ErrNotFound
		}
		// 租户被删除后，会话里的 active_tenant_id 会变成悬空引用，
		// 导致 /auth/session 返回一个不在 tenants 列表中的租户 ID。
		if tx.driver == "sqlite" {
			return tx.sqlite.ClearSessionsActiveTenant(ctx, nullableString(&id))
		}
		return tx.postgres.ClearSessionsActiveTenant(ctx, nullableString(&id))
	})
}
