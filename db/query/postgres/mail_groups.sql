-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/groups.go instead.

-- name: CreateMailGroup :exec
INSERT INTO mail_groups (
    id, tenant_id, name, description, color, sort_order, is_system,
    proxy_url, fallback_proxy_url_1, fallback_proxy_url_2
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10);

-- name: GetMailGroup :one
SELECT * FROM mail_groups WHERE tenant_id = $1 AND id = $2 LIMIT 1;

-- name: GetSystemMailGroup :one
SELECT * FROM mail_groups WHERE tenant_id = $1 AND is_system = 1 LIMIT 1;

-- name: ListMailGroups :many
SELECT * FROM mail_groups WHERE tenant_id = $1 ORDER BY sort_order, created_at;

-- name: CountMailGroups :one
SELECT COUNT(*) FROM mail_groups WHERE tenant_id = $1;

-- name: UpdateMailGroup :execrows
UPDATE mail_groups
SET name = $1, description = $2, color = $3,
    proxy_url = $4, fallback_proxy_url_1 = $5, fallback_proxy_url_2 = $6,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $7 AND id = $8;

-- name: UpdateMailGroupSort :exec
UPDATE mail_groups SET sort_order = $1, updated_at = CURRENT_TIMESTAMP WHERE tenant_id = $2 AND id = $3;

-- name: DeleteMailGroup :execrows
DELETE FROM mail_groups WHERE tenant_id = $1 AND id = $2 AND is_system = 0;

-- name: CountAccountsPerGroup :many
SELECT group_id, COUNT(*) AS account_count
FROM mail_accounts
WHERE tenant_id = $1 AND deleted_at IS NULL
GROUP BY group_id;

-- name: MoveAccountsToGroup :exec
UPDATE mail_accounts
SET group_id = $1, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $2 AND group_id = $3 AND deleted_at IS NULL;
