package repo

import (
	"context"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

// GetAPIKeyByTenant 取租户当前的 Key。没有时返回 ErrNotFound——
// 「还没生成」是正常状态，不是错误，由 service 转成空视图。
func (s *Store) GetAPIKeyByTenant(ctx context.Context, tenantID string) (*model.TenantAPIKey, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetAPIKeyByTenant(ctx, tenantID)
		if e != nil {
			return nil, normalize(e)
		}
		return mapSQLiteAPIKey(v), nil
	}
	v, e := s.postgres.GetAPIKeyByTenant(ctx, tenantID)
	if e != nil {
		return nil, normalize(e)
	}
	return mapPostgresAPIKey(v), nil
}

// GetAPIKeyByHash 按摘要命中 Key，鉴权中间件的唯一查询。
func (s *Store) GetAPIKeyByHash(ctx context.Context, hash string) (*model.TenantAPIKey, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetAPIKeyByHash(ctx, hash)
		if e != nil {
			return nil, normalize(e)
		}
		return mapSQLiteAPIKey(v), nil
	}
	v, e := s.postgres.GetAPIKeyByHash(ctx, hash)
	if e != nil {
		return nil, normalize(e)
	}
	return mapPostgresAPIKey(v), nil
}

// UpsertAPIKey 写入或覆盖租户的 Key。覆盖即吊销：旧摘要被新的顶掉，
// 拿着旧 Key 的调用方下一次请求就会 401。
func (s *Store) UpsertAPIKey(ctx context.Context, v *model.TenantAPIKey) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.UpsertAPIKey(ctx, sqlitedb.UpsertAPIKeyParams{
			TenantID: v.TenantID, TokenHash: v.TokenHash, TokenEnc: v.TokenEnc,
		}))
	}
	return normalize(s.postgres.UpsertAPIKey(ctx, postgresdb.UpsertAPIKeyParams{
		TenantID: v.TenantID, TokenHash: v.TokenHash, TokenEnc: v.TokenEnc,
	}))
}

func mapSQLiteAPIKey(v sqlitedb.TenantApiKey) *model.TenantAPIKey {
	return &model.TenantAPIKey{
		TenantID: v.TenantID, TokenHash: v.TokenHash, TokenEnc: v.TokenEnc,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}

func mapPostgresAPIKey(v postgresdb.TenantApiKey) *model.TenantAPIKey {
	return &model.TenantAPIKey{
		TenantID: v.TenantID, TokenHash: v.TokenHash, TokenEnc: v.TokenEnc,
		CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt,
	}
}
