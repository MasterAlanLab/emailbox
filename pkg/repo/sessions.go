package repo

import (
	"context"
	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
	"emailbox/pkg/model"
)

func (s *Store) CreateSession(ctx context.Context, v *model.Session) error {
	var e error
	if s.driver == "sqlite" {
		e = s.sqlite.CreateSession(ctx, sqlitedb.CreateSessionParams{ID: v.ID, UserID: v.UserID, TokenHash: v.TokenHash, ActiveTenantID: nullableString(v.ActiveTenantID), ExpiresAt: v.ExpiresAt})
	} else {
		e = s.postgres.CreateSession(ctx, postgresdb.CreateSessionParams{ID: v.ID, UserID: v.UserID, TokenHash: v.TokenHash, ActiveTenantID: nullableString(v.ActiveTenantID), ExpiresAt: v.ExpiresAt})
	}
	return normalize(e)
}
func (s *Store) GetSession(ctx context.Context, hash string) (*model.Session, error) {
	if s.driver == "sqlite" {
		v, e := s.sqlite.GetSessionByTokenHash(ctx, hash)
		return &model.Session{ID: v.ID, UserID: v.UserID, TokenHash: v.TokenHash, ActiveTenantID: stringPtr(v.ActiveTenantID), ExpiresAt: v.ExpiresAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}, normalize(e)
	}
	v, e := s.postgres.GetSessionByTokenHash(ctx, hash)
	return &model.Session{ID: v.ID, UserID: v.UserID, TokenHash: v.TokenHash, ActiveTenantID: stringPtr(v.ActiveTenantID), ExpiresAt: v.ExpiresAt, CreatedAt: v.CreatedAt, UpdatedAt: v.UpdatedAt}, normalize(e)
}
func (s *Store) DeleteSession(ctx context.Context, id string) error {
	if s.driver == "sqlite" {
		_, e := s.sqlite.DeleteSession(ctx, id)
		return e
	}
	_, e := s.postgres.DeleteSession(ctx, id)
	return e
}

// DeleteExpiredSessions 清理已过期的会话记录。过期会话不会被认证逻辑接受，
// 但若不清理会无限堆积。
func (s *Store) DeleteExpiredSessions(ctx context.Context) error {
	if s.driver == "sqlite" {
		return s.sqlite.DeleteExpiredSessions(ctx)
	}
	return s.postgres.DeleteExpiredSessions(ctx)
}
func (s *Store) DeleteUserSessions(ctx context.Context, userID string) error {
	if s.driver == "sqlite" {
		return s.sqlite.DeleteSessionsByUserID(ctx, userID)
	}
	return s.postgres.DeleteSessionsByUserID(ctx, userID)
}
func (s *Store) UpdateSessionTenant(ctx context.Context, id string, tenantID *string) error {
	if s.driver == "sqlite" {
		_, e := s.sqlite.UpdateSessionTenant(ctx, sqlitedb.UpdateSessionTenantParams{ActiveTenantID: nullableString(tenantID), ID: id})
		return e
	}
	_, e := s.postgres.UpdateSessionTenant(ctx, postgresdb.UpdateSessionTenantParams{ActiveTenantID: nullableString(tenantID), ID: id})
	return e
}
