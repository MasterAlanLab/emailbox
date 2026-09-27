CREATE TABLE billing_settings (
    id                INTEGER PRIMARY KEY CHECK (id = 1),
    enabled           INTEGER NOT NULL DEFAULT 0,
    mode              TEXT NOT NULL DEFAULT 'test' CHECK (mode IN ('test', 'prod')),
    default_currency  TEXT NOT NULL DEFAULT 'USD',
    updated_by        TEXT REFERENCES users(id),
    updated_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO billing_settings (id) VALUES (1);

CREATE TABLE plan_prices (
    id                    TEXT PRIMARY KEY,
    plan_id               TEXT NOT NULL REFERENCES plans(id) ON DELETE CASCADE,
    billing_period        TEXT NOT NULL CHECK (billing_period IN ('monthly', 'yearly')),
    currency              TEXT NOT NULL,
    amount                TEXT NOT NULL,
    provider_product_id   TEXT NOT NULL DEFAULT '',
    sync_status           TEXT NOT NULL DEFAULT 'pending'
                          CHECK (sync_status IN ('pending', 'active', 'error', 'inactive')),
    sync_error            TEXT NOT NULL DEFAULT '',
    active                INTEGER NOT NULL DEFAULT 1,
    created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (plan_id, billing_period, currency)
);

CREATE TABLE tenant_subscriptions (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL UNIQUE REFERENCES tenants(id) ON DELETE CASCADE,
    provider              TEXT NOT NULL DEFAULT 'waffo',
    mode                  TEXT NOT NULL CHECK (mode IN ('test', 'prod')),
    order_id              TEXT NOT NULL,
    provider_product_id   TEXT NOT NULL,
    plan_price_id         TEXT NOT NULL REFERENCES plan_prices(id),
    plan_id               TEXT NOT NULL REFERENCES plans(id),
    status                TEXT NOT NULL CHECK (status IN ('pending', 'active', 'canceling', 'past_due', 'canceled', 'expired')),
    currency              TEXT NOT NULL,
    amount                TEXT NOT NULL,
    current_period_start  DATETIME,
    current_period_end    DATETIME,
    cancel_at_period_end  INTEGER NOT NULL DEFAULT 0,
    last_event_at         DATETIME,
    created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (provider, mode, order_id)
);

CREATE TABLE billing_checkout_sessions (
    id                    TEXT PRIMARY KEY,
    tenant_id             TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    plan_price_id         TEXT NOT NULL REFERENCES plan_prices(id),
    merchant_external_id TEXT NOT NULL UNIQUE,
    provider_session_id   TEXT NOT NULL DEFAULT '',
    idempotency_key       TEXT NOT NULL,
    checkout_url          TEXT NOT NULL DEFAULT '',
    status                TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'completed', 'expired', 'failed')),
    expires_at            DATETIME,
    created_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at            DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, idempotency_key)
);

CREATE TABLE billing_webhook_events (
    event_id       TEXT NOT NULL,
    mode           TEXT NOT NULL CHECK (mode IN ('test', 'prod')),
    event_type     TEXT NOT NULL,
    store_id       TEXT NOT NULL,
    payload_sha256 TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processed', 'failed')),
    error          TEXT NOT NULL DEFAULT '',
    received_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    processed_at   DATETIME,
    -- Waffo reuses eventId across event types (activated and payment_succeeded
    -- for the same payment share it), so the type is part of the identity.
    PRIMARY KEY (event_type, event_id, mode)
);

ALTER TABLE tenant_quotas ADD COLUMN plan_source TEXT NOT NULL DEFAULT 'admin'
    CHECK (plan_source IN ('admin', 'subscription'));
ALTER TABLE tenant_quotas ADD COLUMN subscription_id TEXT REFERENCES tenant_subscriptions(id);

CREATE INDEX idx_plan_prices_plan ON plan_prices(plan_id, active);
CREATE UNIQUE INDEX idx_plan_prices_provider_product ON plan_prices(provider_product_id)
    WHERE provider_product_id <> '';
CREATE INDEX idx_tenant_subscriptions_status ON tenant_subscriptions(status, current_period_end);
CREATE INDEX idx_billing_checkout_tenant ON billing_checkout_sessions(tenant_id, created_at);
CREATE INDEX idx_billing_webhook_status ON billing_webhook_events(status, received_at);
