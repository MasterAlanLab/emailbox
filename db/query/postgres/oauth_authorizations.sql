-- name: CreateOAuthAuthorization :exec
INSERT INTO oauth_authorizations (
    id, tenant_id, account_id, actor_user_id, state_hash, code_verifier_enc, expires_at
) VALUES ($1, $2, $3, $4, $5, $6, $7);

-- name: GetOAuthAuthorization :one
SELECT * FROM oauth_authorizations
WHERE tenant_id = $1 AND id = $2;

-- name: GetOAuthAuthorizationByState :one
SELECT * FROM oauth_authorizations
WHERE tenant_id = $1 AND id = $2 AND state_hash = $3;

-- name: MarkOAuthAuthorizationExchanged :execrows
UPDATE oauth_authorizations
SET refresh_token_enc = $1, provider_email = $2, status = 'exchanged',
    error_message = '', updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $3 AND id = $4 AND status = 'started' AND expires_at > CURRENT_TIMESTAMP;

-- name: MarkOAuthAuthorizationFailed :execrows
UPDATE oauth_authorizations
SET status = 'failed', error_message = $1, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $2 AND id = $3 AND status IN ('started', 'exchanged');

-- name: ConsumeOAuthAuthorization :execrows
UPDATE oauth_authorizations
SET status = 'consumed', refresh_token_enc = '', code_verifier_enc = '',
    used_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $1 AND id = $2 AND status = 'exchanged' AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteExpiredOAuthAuthorizations :execrows
DELETE FROM oauth_authorizations
WHERE tenant_id = $1 AND expires_at <= CURRENT_TIMESTAMP;
