-- name: CreateSession :exec
INSERT INTO sessions (id, user_id, token_hash, active_tenant_id, expires_at) VALUES (?, ?, ?, ?, ?);
-- name: GetSessionByTokenHash :one
SELECT * FROM sessions WHERE token_hash = ? AND expires_at > CURRENT_TIMESTAMP LIMIT 1;
-- name: UpdateSessionTenant :execrows
UPDATE sessions SET active_tenant_id = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?;
-- name: ClearSessionsActiveTenant :exec
UPDATE sessions SET active_tenant_id = NULL, updated_at = CURRENT_TIMESTAMP WHERE active_tenant_id = ?;
-- name: DeleteSession :execrows
DELETE FROM sessions WHERE id = ?;
-- name: DeleteSessionsByUserID :exec
DELETE FROM sessions WHERE user_id = ?;
-- name: DeleteExpiredSessions :exec
DELETE FROM sessions WHERE expires_at <= CURRENT_TIMESTAMP;
