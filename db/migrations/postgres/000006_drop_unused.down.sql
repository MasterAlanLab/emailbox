-- 只恢复结构，不恢复数据：这些列从未被任何功能写过非默认值。
ALTER TABLE tenant_quotas ADD COLUMN allow_external_api INTEGER;
ALTER TABLE tenant_quotas ADD COLUMN allow_retention INTEGER;
ALTER TABLE tenant_quotas ADD COLUMN allow_forwarding INTEGER;
ALTER TABLE tenant_quotas ADD COLUMN max_share_links INTEGER;
ALTER TABLE tenant_quotas ADD COLUMN max_api_keys INTEGER;

ALTER TABLE plans ADD COLUMN allow_external_api INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN allow_retention INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN allow_forwarding INTEGER NOT NULL DEFAULT 0;
ALTER TABLE plans ADD COLUMN max_share_links INTEGER NOT NULL DEFAULT 10;
ALTER TABLE plans ADD COLUMN max_api_keys INTEGER NOT NULL DEFAULT 2;

ALTER TABLE mail_accounts ADD COLUMN forward_last_checked_at TIMESTAMPTZ;
ALTER TABLE mail_accounts ADD COLUMN forward_cursor TEXT NOT NULL DEFAULT '';
ALTER TABLE mail_accounts ADD COLUMN forward_enabled INTEGER NOT NULL DEFAULT 0;

CREATE INDEX idx_mail_accounts_forward ON mail_accounts(tenant_id, forward_enabled) WHERE forward_enabled = 1;
