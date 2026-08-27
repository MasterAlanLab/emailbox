-- name: CreateUser :exec
INSERT INTO users (id, username, email, password_hash, avatar_url, status) VALUES (?, ?, ?, ?, ?, ?);
-- name: GetUserByID :one
SELECT * FROM users WHERE id = ? AND deleted_at IS NULL LIMIT 1;
-- name: GetUserByUsername :one
SELECT * FROM users WHERE username = ? AND deleted_at IS NULL LIMIT 1;
-- name: UpdateUserProfile :execrows
UPDATE users SET username = ?, email = ?, avatar_url = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL;
-- name: UpdateUserPassword :execrows
UPDATE users SET password_hash = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL;
-- name: UpdateUserPlatformRole :execrows
UPDATE users SET platform_role = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL;
-- name: CountPlatformAdmins :one
SELECT COUNT(*) FROM users WHERE platform_role = 'admin' AND deleted_at IS NULL;
-- name: UpdateUserStatus :execrows
UPDATE users SET status = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ? AND deleted_at IS NULL;
