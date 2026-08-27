-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/groups.go instead.

-- name: CreateMailGroup :exec
INSERT INTO mail_groups (
    id, tenant_id, name, description, color, sort_order, is_system,
    proxy_url, fallback_proxy_url_1, fallback_proxy_url_2
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMailGroup :one
SELECT * FROM mail_groups WHERE tenant_id = ? AND id = ? LIMIT 1;

-- name: GetSystemMailGroup :one
SELECT * FROM mail_groups WHERE tenant_id = ? AND is_system = 1 LIMIT 1;

-- name: ListMailGroups :many
SELECT * FROM mail_groups WHERE tenant_id = ? ORDER BY sort_order, created_at;

-- name: CountMailGroups :one
SELECT COUNT(*) FROM mail_groups WHERE tenant_id = ?;

-- name: UpdateMailGroup :execrows
UPDATE mail_groups
SET name = ?, description = ?, color = ?,
    proxy_url = ?, fallback_proxy_url_1 = ?, fallback_proxy_url_2 = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ?;

-- name: UpdateMailGroupSort :exec
UPDATE mail_groups SET sort_order = ?, updated_at = CURRENT_TIMESTAMP WHERE tenant_id = ? AND id = ?;

-- name: DeleteMailGroup :execrows
DELETE FROM mail_groups WHERE tenant_id = ? AND id = ? AND is_system = 0;

-- name: CountAccountsPerGroup :many
SELECT group_id, COUNT(*) AS account_count
FROM mail_accounts
WHERE tenant_id = ? AND deleted_at IS NULL
GROUP BY group_id;

-- name: MoveAccountsToGroup :exec
UPDATE mail_accounts
SET group_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND group_id = ? AND deleted_at IS NULL;
