-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/groups.go instead.

-- name: CreateMailGroup :exec
INSERT INTO mail_groups (
    id, tenant_id, name, description, color, sort_order, is_system, proxy_url
) VALUES (?, ?, ?, ?, ?, ?, ?, ?);

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
SET name = ?, description = ?, color = ?, proxy_url = ?,
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

-- name: UpdateMailGroupSchedule :execrows
UPDATE mail_groups
SET refresh_interval_minutes = ?, next_refresh_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ?;

-- Scheduler scan. Deliberately not scoped by tenant_id: this is an operational
-- query in the same family as ListStaleJobs and DeleteExpiredSessions, not a
-- business read. The tenant boundary is upheld downstream -- the scheduler only
-- ever submits a job for the tenant_id carried on the row it just read.
-- The EXISTS guard stops the scheduler from acting for a tenant whose user an
-- admin has disabled. It is the only place that check can live: every other
-- refresh path runs behind authentication, which already rejects a disabled
-- user with code 1003. Without it the platform would keep calling the provider
-- every cycle on behalf of an account nobody is allowed to log into.
-- name: ListGroupsDueForRefresh :many
SELECT * FROM mail_groups
WHERE refresh_interval_minutes > 0
  AND next_refresh_at IS NOT NULL
  AND next_refresh_at <= sqlc.arg(due_before)
  AND EXISTS (
      SELECT 1 FROM tenant_members m
      JOIN users u ON u.id = m.user_id
      WHERE m.tenant_id = mail_groups.tenant_id AND u.status = 'active'
  )
ORDER BY next_refresh_at
LIMIT sqlc.arg(row_limit);

-- Claim-by-advancing. The guard repeats the due test instead of comparing the
-- timestamp we read a moment ago: an equality check would hinge on the exact
-- value surviving a round trip through the driver, and it would also miss the
-- two races that actually matter -- the user turning the interval off, or
-- changing it (which recomputes next_refresh_at into the future) between the
-- scan and the claim. Both leave the row failing this WHERE, so rows affected
-- comes back 0 and the caller skips it.
-- name: ClaimGroupRefresh :execrows
UPDATE mail_groups
SET next_refresh_at = sqlc.arg(next_refresh_at)
WHERE id = sqlc.arg(id)
  AND refresh_interval_minutes > 0
  AND next_refresh_at IS NOT NULL
  AND next_refresh_at <= sqlc.arg(due_before);
