-- name: GetBillingSettings :one
SELECT * FROM billing_settings WHERE id = 1;

-- name: UpdateBillingSettings :exec
UPDATE billing_settings
SET enabled = $1, mode = $2, default_currency = $3, updated_by = $4, updated_at = CURRENT_TIMESTAMP
WHERE id = 1;

-- name: ListPlanPrices :many
SELECT pp.*, p.code AS plan_code, p.name AS plan_name
FROM plan_prices pp JOIN plans p ON p.id = pp.plan_id
ORDER BY p.created_at, pp.billing_period, pp.currency;

-- The default plan is what everyone has for free; a price left on it (from
-- before pricing it was rejected) must never show up as something to buy.
-- name: ListActivePlanPrices :many
SELECT pp.*, p.code AS plan_code, p.name AS plan_name
FROM plan_prices pp JOIN plans p ON p.id = pp.plan_id
WHERE pp.active = 1 AND pp.sync_status = 'active' AND p.is_default = 0
ORDER BY p.created_at, pp.billing_period, pp.currency;

-- name: GetPlanPriceByID :one
SELECT pp.*, p.code AS plan_code, p.name AS plan_name
FROM plan_prices pp JOIN plans p ON p.id = pp.plan_id
WHERE pp.id = $1 LIMIT 1;

-- name: CreatePlanPrice :exec
INSERT INTO plan_prices (id, plan_id, billing_period, currency, amount, provider_product_id, sync_status, sync_error, active)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: UpdatePlanPrice :execrows
UPDATE plan_prices
SET amount = $1, currency = $2, sync_status = $3, sync_error = $4, provider_product_id = $5, active = $6, updated_at = CURRENT_TIMESTAMP
WHERE id = $7;

-- name: GetSubscriptionByTenant :one
SELECT * FROM tenant_subscriptions WHERE tenant_id = $1 LIMIT 1;

-- name: UpsertSubscription :exec
INSERT INTO tenant_subscriptions (
 id, tenant_id, provider, mode, order_id, provider_product_id, plan_price_id, plan_id,
 status, currency, amount, current_period_start, current_period_end,
 cancel_at_period_end, last_event_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
ON CONFLICT(tenant_id) DO UPDATE SET
 provider = EXCLUDED.provider, mode = EXCLUDED.mode, order_id = EXCLUDED.order_id,
 provider_product_id = EXCLUDED.provider_product_id, plan_price_id = EXCLUDED.plan_price_id,
 plan_id = EXCLUDED.plan_id, status = EXCLUDED.status, currency = EXCLUDED.currency,
 amount = EXCLUDED.amount, current_period_start = EXCLUDED.current_period_start,
 current_period_end = EXCLUDED.current_period_end, cancel_at_period_end = EXCLUDED.cancel_at_period_end,
 last_event_at = EXCLUDED.last_event_at, updated_at = CURRENT_TIMESTAMP;

-- name: UpdateSubscriptionStatus :execrows
UPDATE tenant_subscriptions
SET status = $1, cancel_at_period_end = $2, current_period_start = $3, current_period_end = $4, last_event_at = $5, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $6;

-- name: CreateCheckoutSession :exec
INSERT INTO billing_checkout_sessions
 (id, tenant_id, plan_price_id, merchant_external_id, provider_session_id, idempotency_key, checkout_url, status, expires_at)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: GetCheckoutByIdempotency :one
SELECT * FROM billing_checkout_sessions WHERE tenant_id = $1 AND idempotency_key = $2 LIMIT 1;

-- name: GetCheckoutByMerchantExternalID :one
SELECT * FROM billing_checkout_sessions WHERE merchant_external_id = $1 LIMIT 1;

-- name: GetSubscriptionByOrderID :one
SELECT * FROM tenant_subscriptions WHERE order_id = $1 LIMIT 1;

-- Marks the checkout that produced an activated subscription. Only pending
-- rows move, so a replayed activation cannot resurrect a failed attempt.
-- Recent checkouts that never got an activation, newest first. The sync
-- endpoint asks Waffo about these when webhooks cannot reach the server.
-- name: ListPendingCheckouts :many
SELECT * FROM billing_checkout_sessions
WHERE tenant_id = $1 AND status = 'pending' AND provider_session_id <> ''
  AND created_at > $2
ORDER BY created_at DESC
LIMIT 5;

-- name: MarkCheckoutCompleted :execrows
UPDATE billing_checkout_sessions
SET status = 'completed', updated_at = CURRENT_TIMESTAMP
WHERE merchant_external_id = $1 AND status = 'pending';

-- name: UpdateCheckoutSession :execrows
UPDATE billing_checkout_sessions
SET provider_session_id = $1, checkout_url = $2, status = $3, expires_at = $4, updated_at = CURRENT_TIMESTAMP
WHERE id = $5;

-- name: InsertWebhookEvent :execrows
INSERT INTO billing_webhook_events (event_id, mode, event_type, store_id, payload_sha256)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT(event_type, event_id, mode) DO NOTHING;

-- name: UpdateWebhookEvent :execrows
UPDATE billing_webhook_events
SET status = $1, error = $2, processed_at = CURRENT_TIMESTAMP
WHERE event_type = $3 AND event_id = $4 AND mode = $5;

-- name: GetWebhookEventStatus :one
SELECT status FROM billing_webhook_events
WHERE event_type = $1 AND event_id = $2 AND mode = $3 LIMIT 1;

-- name: UpdateTenantSubscriptionQuota :execrows
UPDATE tenant_quotas
SET plan_id = $1, plan_source = 'subscription', subscription_id = $2, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $3;

-- name: RestoreTenantQuotaSource :execrows
UPDATE tenant_quotas
SET plan_id = (SELECT id FROM plans WHERE is_default = 1 ORDER BY created_at LIMIT 1),
    plan_source = 'admin', subscription_id = NULL, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = $1 AND plan_source = 'subscription' AND subscription_id = $2;
