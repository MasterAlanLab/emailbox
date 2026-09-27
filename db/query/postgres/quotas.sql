-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/quotas.go instead.

-- name: CreateTenantQuota :exec
INSERT INTO tenant_quotas (tenant_id, plan_id) VALUES ($1, $2);

-- Limits come straight from the plan: there are no per-tenant overrides.
-- An admin who wants a tenant to have more assigns it a different plan.
-- name: GetEffectiveQuota :one
SELECT
    tq.plan_id       AS plan_id,
    tq.plan_source   AS plan_source,
    pl.code          AS plan_code,
    pl.name          AS plan_name,
    pl.max_accounts  AS max_accounts,
    pl.max_groups    AS max_groups,
    pl.daily_mail_fetch AS daily_mail_fetch
FROM tenant_quotas AS tq
JOIN plans AS pl ON pl.id = tq.plan_id
WHERE tq.tenant_id = $1;

-- name: GetUsageCount :one
SELECT count FROM usage_counters WHERE tenant_id = $1 AND day = $2 AND metric = $3;

-- name: ConsumeUsage :one
INSERT INTO usage_counters (tenant_id, day, metric, count) VALUES ($1, $2, $3, $4)
ON CONFLICT (tenant_id, day, metric)
DO UPDATE SET count = usage_counters.count + excluded.count
RETURNING count;

-- An admin picking a plan takes ownership of it: plan_source goes back to
-- admin, so a later subscription cancellation will not revert this choice.
-- name: UpdateTenantPlan :execrows
UPDATE tenant_quotas
SET plan_id = $1, plan_source = 'admin', subscription_id = NULL, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $2;
