ALTER TABLE mail_accounts ADD COLUMN last_refresh_error_kind TEXT NOT NULL DEFAULT '';

CREATE TABLE oauth_authorizations (
    id                  TEXT PRIMARY KEY,
    tenant_id           TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id          TEXT NOT NULL REFERENCES mail_accounts(id) ON DELETE CASCADE,
    actor_user_id       TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    state_hash          TEXT NOT NULL UNIQUE,
    code_verifier_enc   TEXT NOT NULL,
    refresh_token_enc   TEXT NOT NULL DEFAULT '',
    provider_email      TEXT NOT NULL DEFAULT '',
    status              TEXT NOT NULL DEFAULT 'started'
                        CHECK (status IN ('started', 'exchanged', 'consumed', 'failed')),
    error_message       TEXT NOT NULL DEFAULT '',
    expires_at          DATETIME NOT NULL,
    used_at             DATETIME,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_oauth_authorizations_account
    ON oauth_authorizations(tenant_id, account_id, status, expires_at);
