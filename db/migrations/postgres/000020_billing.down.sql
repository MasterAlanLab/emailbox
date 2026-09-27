ALTER TABLE tenant_quotas DROP COLUMN subscription_id;
ALTER TABLE tenant_quotas DROP COLUMN plan_source;
DROP TABLE billing_webhook_events;
DROP TABLE billing_checkout_sessions;
DROP TABLE tenant_subscriptions;
DROP TABLE plan_prices;
DROP TABLE billing_settings;
