-- name: CreateUser :exec
INSERT INTO users (id, username, email, password_hash, status) VALUES ($1, $2, $3, $4, $5);
-- name: GetUserByID :one
SELECT * FROM users WHERE id = $1 AND deleted_at IS NULL LIMIT 1;
-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = $1 AND deleted_at IS NULL LIMIT 1;
-- name: UpdateUserProfile :execrows
UPDATE users SET username = $1, email = $2, updated_at = CURRENT_TIMESTAMP WHERE id = $3 AND deleted_at IS NULL;
-- name: UpdateUserPassword :execrows
UPDATE users SET password_hash = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;
-- name: UpdateUserPlatformRole :execrows
UPDATE users SET platform_role = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;
-- name: CountPlatformAdmins :one
SELECT COUNT(*) FROM users WHERE platform_role = 'admin' AND deleted_at IS NULL;
-- name: UpdateUserStatus :execrows
UPDATE users SET status = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2 AND deleted_at IS NULL;
