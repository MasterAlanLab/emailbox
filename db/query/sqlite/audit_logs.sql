-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/audit.go instead.

-- name: CreateAuditLog :exec
INSERT INTO audit_logs (
    id, tenant_id, actor_user_id, actor_name, actor_kind,
    action, resource_type, resource_id, ip, details
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- Filters are all optional. Tie-break on id so pages stay stable when several
-- rows share a timestamp (a batch operation writes many rows in one second).
-- name: ListAuditLogsPage :many
SELECT * FROM audit_logs
WHERE (sqlc.narg(tenant_id) IS NULL OR tenant_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(actor_user_id) IS NULL OR actor_user_id = sqlc.narg(actor_user_id))
  AND (sqlc.narg(actor_kind) IS NULL OR actor_kind = sqlc.narg(actor_kind))
  AND (sqlc.narg(action) IS NULL OR action = sqlc.narg(action))
ORDER BY created_at DESC, id DESC
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountAuditLogs :one
SELECT COUNT(*) FROM audit_logs
WHERE (sqlc.narg(tenant_id) IS NULL OR tenant_id = sqlc.narg(tenant_id))
  AND (sqlc.narg(actor_user_id) IS NULL OR actor_user_id = sqlc.narg(actor_user_id))
  AND (sqlc.narg(actor_kind) IS NULL OR actor_kind = sqlc.narg(actor_kind))
  AND (sqlc.narg(action) IS NULL OR action = sqlc.narg(action));
