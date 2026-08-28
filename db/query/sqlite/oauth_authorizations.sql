-- name: CreateOAuthAuthorization :exec
INSERT INTO oauth_authorizations (
    id, tenant_id, account_id, actor_user_id, state_hash, code_verifier_enc, expires_at
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: GetOAuthAuthorization :one
SELECT * FROM oauth_authorizations
WHERE tenant_id = ? AND id = ?;

-- name: GetOAuthAuthorizationByState :one
SELECT * FROM oauth_authorizations
WHERE tenant_id = ? AND id = ? AND state_hash = ?;

-- name: MarkOAuthAuthorizationExchanged :execrows
UPDATE oauth_authorizations
SET refresh_token_enc = ?, provider_email = ?, status = 'exchanged',
    error_message = '', updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ? AND status = 'started' AND expires_at > CURRENT_TIMESTAMP;

-- name: MarkOAuthAuthorizationFailed :execrows
UPDATE oauth_authorizations
SET status = 'failed', error_message = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ? AND status IN ('started', 'exchanged');

-- name: ConsumeOAuthAuthorization :execrows
UPDATE oauth_authorizations
SET status = 'consumed', refresh_token_enc = '', code_verifier_enc = '',
    used_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ? AND status = 'exchanged' AND expires_at > CURRENT_TIMESTAMP;

-- name: DeleteExpiredOAuthAuthorizations :execrows
DELETE FROM oauth_authorizations
WHERE tenant_id = ? AND expires_at <= CURRENT_TIMESTAMP;
