-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, active_tenant_id, expires_at) VALUES ($1, $2, $3, $4, $5);
-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = $1 AND expires_at > CURRENT_TIMESTAMP LIMIT 1;
-- name: UpdateSessionTenant :execrows
UPDATE sessions SET active_tenant_id = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2;
-- name: ClearSessionsActiveTenant :exec
UPDATE sessions SET active_tenant_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE active_tenant_id = $1;
-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = $1;
-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions WHERE user_id = $1;
-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP;
