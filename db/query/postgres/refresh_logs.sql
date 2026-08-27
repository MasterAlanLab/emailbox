-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/refresh_logs.go instead.

-- name: CreateRefreshLog :exec
INSERT INTO mail_refresh_logs (
    id, tenant_id, account_id, account_email, job_id,
    refresh_type, status, error_kind, error_message
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListRefreshLogsPage :many
SELECT * FROM mail_refresh_logs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(account_id)::text IS NULL OR account_id = sqlc.narg(account_id)::text)
  AND (sqlc.narg(job_id)::text IS NULL OR job_id = sqlc.narg(job_id)::text)
ORDER BY created_at DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountRefreshLogs :one
SELECT COUNT(*) FROM mail_refresh_logs
WHERE tenant_id = sqlc.arg(tenant_id)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  AND (sqlc.narg(account_id)::text IS NULL OR account_id = sqlc.narg(account_id)::text)
  AND (sqlc.narg(job_id)::text IS NULL OR job_id = sqlc.narg(job_id)::text);

-- The four headline numbers come from mail_accounts, not from the log table:
-- they answer "what is the state of my mailboxes right now", which is a property
-- of the account, not a count of historical attempts.
-- name: GetRefreshStats :one
SELECT
    COUNT(*) AS total,
    SUM(CASE WHEN last_refresh_status = 'success' THEN 1 ELSE 0 END) AS success,
    SUM(CASE WHEN last_refresh_status = 'failed'  THEN 1 ELSE 0 END) AS failed,
    SUM(CASE WHEN last_refresh_status = 'never'   THEN 1 ELSE 0 END) AS never_refreshed
FROM mail_accounts
WHERE tenant_id = $1 AND deleted_at IS NULL;

-- Failure breakdown comes from the log table because mail_accounts only keeps
-- the error text, not its kind. Bounded by a time window so the aggregate does
-- not slowly turn into a full-table scan as history accumulates.
-- name: GroupRefreshFailuresByKind :many
SELECT error_kind, COUNT(*) AS count
FROM mail_refresh_logs
WHERE tenant_id = $1 AND status = 'failed' AND created_at >= $2
GROUP BY error_kind
ORDER BY count DESC;
