-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/admin.go instead.

-- The personal tenant is joined in so the admin list can link straight to a
-- user's mailboxes, and account_count comes from a correlated subquery rather
-- than a GROUP BY: users without a tenant must still show up as a row.
-- name: ListAdminUsersPage :many
SELECT u.id, u.username, u.email, u.status, u.platform_role,
       u.created_at, u.last_login_at,
       COALESCE(t.id, '')   AS tenant_id,
       COALESCE(t.name, '') AS tenant_name,
       COALESCE(pl.code, '') AS plan_code,
       COALESCE(tq.max_accounts, pl.max_accounts, -1) AS max_accounts,
       (SELECT COUNT(*) FROM mail_accounts ma
         WHERE ma.tenant_id = t.id AND ma.deleted_at IS NULL) AS account_count
FROM users u
LEFT JOIN tenants t
       ON t.created_by = u.id AND t.kind = 'personal' AND t.deleted_at IS NULL
LEFT JOIN tenant_quotas tq ON tq.tenant_id = t.id
LEFT JOIN plans pl ON pl.id = tq.plan_id
WHERE u.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR u.status = sqlc.narg(status)::text)
  AND (sqlc.narg(platform_role)::text IS NULL OR u.platform_role = sqlc.narg(platform_role)::text)
  AND (sqlc.narg(q)::text IS NULL
       OR strpos(lower(u.username), sqlc.narg(q)::text) > 0
       OR strpos(lower(u.email), sqlc.narg(q)::text) > 0)
ORDER BY u.created_at DESC, u.id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountAdminUsers :one
SELECT COUNT(*) FROM users u
WHERE u.deleted_at IS NULL
  AND (sqlc.narg(status)::text IS NULL OR u.status = sqlc.narg(status)::text)
  AND (sqlc.narg(platform_role)::text IS NULL OR u.platform_role = sqlc.narg(platform_role)::text)
  AND (sqlc.narg(q)::text IS NULL
       OR strpos(lower(u.username), sqlc.narg(q)::text) > 0
       OR strpos(lower(u.email), sqlc.narg(q)::text) > 0);

-- name: GetAdminUser :one
SELECT u.id, u.username, u.email, u.status, u.platform_role,
       u.created_at, u.last_login_at,
       COALESCE(t.id, '')   AS tenant_id,
       COALESCE(t.name, '') AS tenant_name,
       COALESCE(pl.code, '') AS plan_code,
       COALESCE(tq.max_accounts, pl.max_accounts, -1) AS max_accounts,
       (SELECT COUNT(*) FROM mail_accounts ma
         WHERE ma.tenant_id = t.id AND ma.deleted_at IS NULL) AS account_count
FROM users u
LEFT JOIN tenants t
       ON t.created_by = u.id AND t.kind = 'personal' AND t.deleted_at IS NULL
LEFT JOIN tenant_quotas tq ON tq.tenant_id = t.id
LEFT JOIN plans pl ON pl.id = tq.plan_id
WHERE u.id = $1 AND u.deleted_at IS NULL
LIMIT 1;

-- name: UpdateUserLastLogin :exec
UPDATE users SET last_login_at = CURRENT_TIMESTAMP WHERE id = $1;

-- name: SoftDeleteUser :execrows
UPDATE users
SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP
WHERE id = $1 AND deleted_at IS NULL;

-- max_accounts is resolved here (override first, then plan) so the list can
-- flag tenants sitting over their limit after an admin lowered it.
-- name: ListAdminTenantsPage :many
SELECT t.id, t.name, t.slug, t.kind, t.created_at,
       COALESCE(u.id, '')    AS owner_user_id,
       COALESCE(u.email, '') AS owner_email,
       COALESCE(pl.code, '') AS plan_code,
       COALESCE(tq.max_accounts, pl.max_accounts, -1) AS max_accounts,
       (SELECT COUNT(*) FROM mail_accounts ma
         WHERE ma.tenant_id = t.id AND ma.deleted_at IS NULL) AS account_count
FROM tenants t
LEFT JOIN users u ON u.id = t.created_by
LEFT JOIN tenant_quotas tq ON tq.tenant_id = t.id
LEFT JOIN plans pl ON pl.id = tq.plan_id
WHERE t.deleted_at IS NULL
  AND (sqlc.narg(q)::text IS NULL
       OR strpos(lower(t.name), sqlc.narg(q)::text) > 0
       OR strpos(lower(u.email), sqlc.narg(q)::text) > 0)
ORDER BY t.created_at DESC, t.id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: CountAdminTenants :one
SELECT COUNT(*) FROM tenants t
LEFT JOIN users u ON u.id = t.created_by
WHERE t.deleted_at IS NULL
  AND (sqlc.narg(q)::text IS NULL
       OR strpos(lower(t.name), sqlc.narg(q)::text) > 0
       OR strpos(lower(u.email), sqlc.narg(q)::text) > 0);

-- One round trip for the whole overview card. Each sub-select is an indexed
-- count; running eight of them separately would be eight round trips for a
-- page that is refreshed on every admin visit.
-- name: GetPlatformStats :one
SELECT
    (SELECT COUNT(*) FROM users WHERE deleted_at IS NULL) AS user_count,
    (SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND status = 'disabled') AS disabled_user_count,
    (SELECT COUNT(*) FROM users WHERE deleted_at IS NULL AND platform_role = 'admin') AS admin_count,
    (SELECT COUNT(*) FROM tenants WHERE deleted_at IS NULL) AS tenant_count,
    (SELECT COUNT(*) FROM mail_accounts WHERE deleted_at IS NULL) AS account_count,
    (SELECT COUNT(*) FROM mail_accounts WHERE deleted_at IS NULL AND status = 'banned') AS banned_account_count,
    (SELECT COALESCE(SUM(uc.count), 0) FROM usage_counters uc
      WHERE uc.day = sqlc.arg(for_day)::text AND uc.metric = 'mail_fetch') AS mail_fetch_today,
    (SELECT COALESCE(SUM(uc.count), 0) FROM usage_counters uc
      WHERE uc.day = sqlc.arg(for_day)::text AND uc.metric = 'token_refresh') AS token_refresh_today;
