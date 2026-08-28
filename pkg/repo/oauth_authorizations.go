package repo

import (
	"context"
	"time"

	postgresdb "emailbox/db/generated/postgres"
	sqlitedb "emailbox/db/generated/sqlite"
)

// OAuthAuthorization 是一次短期、一次性的 Microsoft 授权流程。
// 两个令牌字段始终保存密文，上层只有在交换和提交时短暂解密。
type OAuthAuthorization struct {
	ID, TenantID, AccountID, ActorUserID string
	StateHash, CodeVerifierEnc           string
	RefreshTokenEnc, ProviderEmail       string
	Status, ErrorMessage                 string
	ExpiresAt                            time.Time
}

func (s *Store) CreateOAuthAuthorization(ctx context.Context, f OAuthAuthorization) error {
	if s.driver == "sqlite" {
		return normalize(s.sqlite.CreateOAuthAuthorization(ctx, sqlitedb.CreateOAuthAuthorizationParams{
			ID: f.ID, TenantID: f.TenantID, AccountID: f.AccountID, ActorUserID: f.ActorUserID,
			StateHash: f.StateHash, CodeVerifierEnc: f.CodeVerifierEnc, ExpiresAt: f.ExpiresAt,
		}))
	}
	return normalize(s.postgres.CreateOAuthAuthorization(ctx, postgresdb.CreateOAuthAuthorizationParams{
		ID: f.ID, TenantID: f.TenantID, AccountID: f.AccountID, ActorUserID: f.ActorUserID,
		StateHash: f.StateHash, CodeVerifierEnc: f.CodeVerifierEnc, ExpiresAt: f.ExpiresAt,
	}))
}

func (s *Store) GetOAuthAuthorization(ctx context.Context, tenantID, id string) (*OAuthAuthorization, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetOAuthAuthorization(ctx, sqlitedb.GetOAuthAuthorizationParams{TenantID: tenantID, ID: id})
		if err != nil {
			return nil, normalize(err)
		}
		return mapSQLiteOAuthAuthorization(v), nil
	}
	v, err := s.postgres.GetOAuthAuthorization(ctx, postgresdb.GetOAuthAuthorizationParams{TenantID: tenantID, ID: id})
	if err != nil {
		return nil, normalize(err)
	}
	return mapPostgresOAuthAuthorization(v), nil
}

func (s *Store) GetOAuthAuthorizationByState(ctx context.Context, tenantID, id, stateHash string) (*OAuthAuthorization, error) {
	if s.driver == "sqlite" {
		v, err := s.sqlite.GetOAuthAuthorizationByState(ctx, sqlitedb.GetOAuthAuthorizationByStateParams{TenantID: tenantID, ID: id, StateHash: stateHash})
		if err != nil {
			return nil, normalize(err)
		}
		return mapSQLiteOAuthAuthorization(v), nil
	}
	v, err := s.postgres.GetOAuthAuthorizationByState(ctx, postgresdb.GetOAuthAuthorizationByStateParams{TenantID: tenantID, ID: id, StateHash: stateHash})
	if err != nil {
		return nil, normalize(err)
	}
	return mapPostgresOAuthAuthorization(v), nil
}

func (s *Store) MarkOAuthAuthorizationExchanged(ctx context.Context, tenantID, id, tokenEnc, email string) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.MarkOAuthAuthorizationExchanged(ctx, sqlitedb.MarkOAuthAuthorizationExchangedParams{RefreshTokenEnc: tokenEnc, ProviderEmail: email, TenantID: tenantID, ID: id})
	} else {
		n, err = s.postgres.MarkOAuthAuthorizationExchanged(ctx, postgresdb.MarkOAuthAuthorizationExchangedParams{RefreshTokenEnc: tokenEnc, ProviderEmail: email, TenantID: tenantID, ID: id})
	}
	return rowsAffected(n, err)
}

func (s *Store) MarkOAuthAuthorizationFailed(ctx context.Context, tenantID, id, message string) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.MarkOAuthAuthorizationFailed(ctx, sqlitedb.MarkOAuthAuthorizationFailedParams{ErrorMessage: message, TenantID: tenantID, ID: id})
	} else {
		n, err = s.postgres.MarkOAuthAuthorizationFailed(ctx, postgresdb.MarkOAuthAuthorizationFailedParams{ErrorMessage: message, TenantID: tenantID, ID: id})
	}
	return rowsAffected(n, err)
}

func (s *Store) ConsumeOAuthAuthorization(ctx context.Context, tenantID, id string) error {
	var n int64
	var err error
	if s.driver == "sqlite" {
		n, err = s.sqlite.ConsumeOAuthAuthorization(ctx, sqlitedb.ConsumeOAuthAuthorizationParams{TenantID: tenantID, ID: id})
	} else {
		n, err = s.postgres.ConsumeOAuthAuthorization(ctx, postgresdb.ConsumeOAuthAuthorizationParams{TenantID: tenantID, ID: id})
	}
	return rowsAffected(n, err)
}

func (s *Store) DeleteExpiredOAuthAuthorizations(ctx context.Context, tenantID string) (int, error) {
	if s.driver == "sqlite" {
		n, err := s.sqlite.DeleteExpiredOAuthAuthorizations(ctx, tenantID)
		return int(n), normalize(err)
	}
	n, err := s.postgres.DeleteExpiredOAuthAuthorizations(ctx, tenantID)
	return int(n), normalize(err)
}

func mapSQLiteOAuthAuthorization(v sqlitedb.OauthAuthorization) *OAuthAuthorization {
	return &OAuthAuthorization{ID: v.ID, TenantID: v.TenantID, AccountID: v.AccountID, ActorUserID: v.ActorUserID,
		StateHash: v.StateHash, CodeVerifierEnc: v.CodeVerifierEnc, RefreshTokenEnc: v.RefreshTokenEnc,
		ProviderEmail: v.ProviderEmail, Status: v.Status, ErrorMessage: v.ErrorMessage, ExpiresAt: v.ExpiresAt}
}

func mapPostgresOAuthAuthorization(v postgresdb.OauthAuthorization) *OAuthAuthorization {
	return &OAuthAuthorization{ID: v.ID, TenantID: v.TenantID, AccountID: v.AccountID, ActorUserID: v.ActorUserID,
		StateHash: v.StateHash, CodeVerifierEnc: v.CodeVerifierEnc, RefreshTokenEnc: v.RefreshTokenEnc,
		ProviderEmail: v.ProviderEmail, Status: v.Status, ErrorMessage: v.ErrorMessage, ExpiresAt: v.ExpiresAt}
}
