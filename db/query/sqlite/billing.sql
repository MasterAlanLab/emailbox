-- name: GetBillingSettings :one
SELECT * FROM billing_settings WHERE id = 1;

-- name: UpdateBillingSettings :exec
UPDATE billing_settings
SET enabled = ?, mode = ?, default_currency = ?, updated_by = ?, updated_at = CURRENT_TIMESTAMP
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
WHERE pp.id = ? LIMIT 1;

-- name: CreatePlanPrice :exec
INSERT INTO plan_prices (id, plan_id, billing_period, currency, amount, provider_product_id, sync_status, sync_error, active)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: UpdatePlanPrice :execrows
UPDATE plan_prices
SET amount = ?, currency = ?, sync_status = ?, sync_error = ?, provider_product_id = ?, active = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: GetSubscriptionByTenant :one
SELECT * FROM tenant_subscriptions WHERE tenant_id = ? LIMIT 1;

-- name: UpsertSubscription :exec
INSERT INTO tenant_subscriptions (
 id, tenant_id, provider, mode, order_id, provider_product_id, plan_price_id, plan_id,
 status, currency, amount, current_period_start, current_period_end,
 cancel_at_period_end, last_event_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(tenant_id) DO UPDATE SET
 provider = excluded.provider, mode = excluded.mode, order_id = excluded.order_id,
 provider_product_id = excluded.provider_product_id, plan_price_id = excluded.plan_price_id,
 plan_id = excluded.plan_id, status = excluded.status, currency = excluded.currency,
 amount = excluded.amount, current_period_start = excluded.current_period_start,
 current_period_end = excluded.current_period_end, cancel_at_period_end = excluded.cancel_at_period_end,
 last_event_at = excluded.last_event_at, updated_at = CURRENT_TIMESTAMP;

-- name: UpdateSubscriptionStatus :execrows
UPDATE tenant_subscriptions
SET status = ?, cancel_at_period_end = ?, current_period_start = ?, current_period_end = ?, last_event_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ?;

-- name: CreateCheckoutSession :exec
INSERT INTO billing_checkout_sessions
 (id, tenant_id, plan_price_id, merchant_external_id, provider_session_id, idempotency_key, checkout_url, status, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetCheckoutByIdempotency :one
SELECT * FROM billing_checkout_sessions WHERE tenant_id = ? AND idempotency_key = ? LIMIT 1;

-- name: GetCheckoutByMerchantExternalID :one
SELECT * FROM billing_checkout_sessions WHERE merchant_external_id = ? LIMIT 1;

-- name: GetSubscriptionByOrderID :one
SELECT * FROM tenant_subscriptions WHERE order_id = ? LIMIT 1;

-- Marks the checkout that produced an activated subscription. Only pending
-- rows move, so a replayed activation cannot resurrect a failed attempt.
-- Recent checkouts that never got an activation, newest first. The sync
-- endpoint asks Waffo about these when webhooks cannot reach the server.
-- name: ListPendingCheckouts :many
SELECT * FROM billing_checkout_sessions
WHERE tenant_id = ? AND status = 'pending' AND provider_session_id <> ''
  AND created_at > ?
ORDER BY created_at DESC
LIMIT 5;

-- name: MarkCheckoutCompleted :execrows
UPDATE billing_checkout_sessions
SET status = 'completed', updated_at = CURRENT_TIMESTAMP
WHERE merchant_external_id = ? AND status = 'pending';

-- name: UpdateCheckoutSession :execrows
UPDATE billing_checkout_sessions
SET provider_session_id = ?, checkout_url = ?, status = ?, expires_at = ?, updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: InsertWebhookEvent :execrows
INSERT INTO billing_webhook_events (event_id, mode, event_type, store_id, payload_sha256)
VALUES (?, ?, ?, ?, ?)
ON CONFLICT(event_type, event_id, mode) DO NOTHING;

-- name: UpdateWebhookEvent :execrows
UPDATE billing_webhook_events
SET status = ?, error = ?, processed_at = CURRENT_TIMESTAMP
WHERE event_type = ? AND event_id = ? AND mode = ?;

-- name: GetWebhookEventStatus :one
SELECT status FROM billing_webhook_events
WHERE event_type = ? AND event_id = ? AND mode = ? LIMIT 1;

-- name: UpdateTenantSubscriptionQuota :execrows
UPDATE tenant_quotas
SET plan_id = ?, plan_source = 'subscription', subscription_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ?;

-- name: RestoreTenantQuotaSource :execrows
UPDATE tenant_quotas
SET plan_id = (SELECT id FROM plans WHERE is_default = 1 ORDER BY created_at LIMIT 1),
    plan_source = 'admin', subscription_id = NULL, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND plan_source = 'subscription' AND subscription_id = ?;
