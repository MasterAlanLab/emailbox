ALTER TABLE tenant_quotas ADD COLUMN updated_by TEXT REFERENCES users(id);
ALTER TABLE tenant_quotas ADD COLUMN note TEXT NOT NULL DEFAULT '';
ALTER TABLE tenant_quotas ADD COLUMN daily_mail_fetch INTEGER;
ALTER TABLE tenant_quotas ADD COLUMN max_groups INTEGER;
ALTER TABLE tenant_quotas ADD COLUMN max_accounts INTEGER;
