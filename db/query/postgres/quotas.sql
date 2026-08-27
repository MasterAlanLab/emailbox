-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/quotas.go instead.

-- name: CreateTenantQuota :exec
INSERT INTO tenant_quotas (tenant_id, plan_id) VALUES ($1, $2);

-- name: GetEffectiveQuota :one
SELECT
    pl.code                                                  AS plan_code,
    pl.name                                                  AS plan_name,
    COALESCE(tq.max_accounts, pl.max_accounts)               AS max_accounts,
    COALESCE(tq.max_groups, pl.max_groups)                   AS max_groups,
    COALESCE(tq.daily_mail_fetch, pl.daily_mail_fetch)       AS daily_mail_fetch,
    COALESCE(tq.daily_token_refresh, pl.daily_token_refresh) AS daily_token_refresh
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

-- Admin overrides. NULL in a column means "fall back to the plan value".
-- name: UpdateTenantQuotaOverrides :execrows
UPDATE tenant_quotas
SET max_accounts = $1, max_groups = $2,
    daily_mail_fetch = $3, daily_token_refresh = $4,
    note = $5, updated_by = $6, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $7;

-- name: UpdateTenantPlan :execrows
UPDATE tenant_quotas SET plan_id = $1, updated_at = CURRENT_TIMESTAMP WHERE tenant_id = $2;
