-- migrate:no-transaction
-- 取消 Graph 通道，并增加 Gmail IMAP OAuth 通道。
PRAGMA foreign_keys = OFF;
BEGIN;

DROP TABLE IF EXISTS mail_accounts_new;
CREATE TABLE mail_accounts_new (
    id                       TEXT PRIMARY KEY,
    tenant_id                TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    group_id                 TEXT NOT NULL REFERENCES mail_groups(id),
    email                    TEXT NOT NULL,
    email_normalized         TEXT NOT NULL,
    provider                 TEXT NOT NULL DEFAULT 'outlook',
    account_type             TEXT NOT NULL DEFAULT 'outlook'
                             CHECK (account_type IN ('outlook', 'imap')),
    auth_channel             TEXT NOT NULL DEFAULT ''
                             CHECK (auth_channel IN ('', 'imap_new', 'imap_old', 'imap_gmail', 'imap')),
    password_enc             TEXT NOT NULL DEFAULT '',
    client_id                TEXT NOT NULL DEFAULT '',
    refresh_token_enc        TEXT NOT NULL DEFAULT '',
    imap_host                TEXT NOT NULL DEFAULT '',
    imap_port                INTEGER NOT NULL DEFAULT 993,
    imap_password_enc        TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL DEFAULT 'active'
                             CHECK (status IN ('active', 'disabled', 'banned')),
    remark                   TEXT NOT NULL DEFAULT '',
    sort_order               INTEGER NOT NULL DEFAULT 0,
    proxy_url                TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_1    TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_2    TEXT NOT NULL DEFAULT '',
    last_refresh_at          DATETIME,
    last_refresh_status      TEXT NOT NULL DEFAULT 'never'
                             CHECK (last_refresh_status IN ('never', 'success', 'failed')),
    last_refresh_error       TEXT NOT NULL DEFAULT '',
    refresh_token_updated_at DATETIME,
    created_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at               DATETIME,
    last_refresh_error_kind  TEXT NOT NULL DEFAULT ''
);

INSERT INTO mail_accounts_new (
    id, tenant_id, group_id, email, email_normalized, provider, account_type,
    auth_channel, password_enc, client_id, refresh_token_enc, imap_host, imap_port,
    imap_password_enc, status, remark, sort_order, proxy_url, fallback_proxy_url_1,
    fallback_proxy_url_2, last_refresh_at, last_refresh_status, last_refresh_error,
    refresh_token_updated_at, created_at, updated_at, deleted_at, last_refresh_error_kind
)
SELECT id, tenant_id, group_id, email, email_normalized, provider, account_type,
       CASE WHEN auth_channel = 'graph' THEN '' ELSE auth_channel END,
       password_enc, client_id, refresh_token_enc, imap_host, imap_port,
       imap_password_enc, status, remark, sort_order, proxy_url, fallback_proxy_url_1,
       fallback_proxy_url_2, last_refresh_at, last_refresh_status, last_refresh_error,
       refresh_token_updated_at, created_at, updated_at, deleted_at, last_refresh_error_kind
FROM mail_accounts;

DROP TABLE mail_accounts;
ALTER TABLE mail_accounts_new RENAME TO mail_accounts;
CREATE UNIQUE INDEX idx_mail_accounts_tenant_email ON mail_accounts(tenant_id, email_normalized)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_mail_accounts_group ON mail_accounts(tenant_id, group_id, sort_order);
CREATE INDEX idx_mail_accounts_refresh ON mail_accounts(tenant_id, last_refresh_status, last_refresh_at);
CREATE INDEX idx_mail_accounts_status ON mail_accounts(tenant_id, status);

COMMIT;
PRAGMA foreign_key_check;
PRAGMA foreign_keys = ON;
