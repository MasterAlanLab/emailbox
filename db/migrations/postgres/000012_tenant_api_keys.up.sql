-- 对外取件用的 API Key，说明见 sqlite/000012_tenant_api_keys.up.sql。
CREATE TABLE tenant_api_keys (
    tenant_id  TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    token_hash TEXT NOT NULL UNIQUE,
    token_enc  TEXT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);
