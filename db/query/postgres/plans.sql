-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/quotas.go instead.

-- name: GetPlanByID :one
SELECT * FROM plans WHERE id = $1 LIMIT 1;
-- name: GetPlanByCode :one
SELECT * FROM plans WHERE code = $1 LIMIT 1;
-- name: GetDefaultPlan :one
SELECT * FROM plans WHERE is_default = 1 ORDER BY created_at LIMIT 1;
-- name: ListPlans :many
SELECT * FROM plans ORDER BY created_at;

-- name: CreatePlan :exec
INSERT INTO plans (
    id, code, name, is_default,
    max_accounts, max_groups,
    daily_mail_fetch, daily_token_refresh
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8);

-- code is not updatable: it is the stable identifier other systems key off.
-- name: UpdatePlan :execrows
UPDATE plans
SET name = $1, is_default = $2,
    max_accounts = $3, max_groups = $4,
    daily_mail_fetch = $5, daily_token_refresh = $6,
    updated_at = CURRENT_TIMESTAMP
WHERE id = $7;

-- name: DeletePlan :execrows
DELETE FROM plans WHERE id = $1;

-- Guards the delete: a plan still referenced by tenant_quotas must not go away,
-- otherwise those tenants lose the row their effective quota is derived from.
-- name: CountTenantsByPlan :one
SELECT COUNT(*) FROM tenant_quotas WHERE plan_id = $1;

-- name: ClearDefaultPlanExcept :exec
UPDATE plans SET is_default = 0, updated_at = CURRENT_TIMESTAMP
WHERE is_default = 1 AND id <> $1;
