-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/quotas.go instead.

-- name: GetPlanByID :one
SELECT * FROM plans WHERE id = ? LIMIT 1;
-- name: GetPlanByCode :one
SELECT * FROM plans WHERE code = ? LIMIT 1;
-- name: GetDefaultPlan :one
SELECT * FROM plans WHERE is_default = 1 ORDER BY created_at LIMIT 1;
-- name: ListPlans :many
SELECT * FROM plans ORDER BY created_at;

-- name: CreatePlan :exec
INSERT INTO plans (
    id, code, name, is_default,
    max_accounts, max_groups,
    daily_mail_fetch
) VALUES (?, ?, ?, ?, ?, ?, ?);

-- code is not updatable: it is the stable identifier other systems key off.
-- name: UpdatePlan :execrows
UPDATE plans
SET name = ?, is_default = ?,
    max_accounts = ?, max_groups = ?,
    daily_mail_fetch = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeletePlan :execrows
DELETE FROM plans WHERE id = ?;

-- Guards the delete: a plan still referenced by tenant_quotas must not go away,
-- otherwise those tenants lose the row their effective quota is derived from.
-- name: CountTenantsByPlan :one
SELECT COUNT(*) FROM tenant_quotas WHERE plan_id = ?;

-- name: ClearDefaultPlanExcept :exec
UPDATE plans SET is_default = 0, updated_at = CURRENT_TIMESTAMP
WHERE is_default = 1 AND id <> ?;
