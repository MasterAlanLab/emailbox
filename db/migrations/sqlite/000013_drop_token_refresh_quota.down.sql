-- 只恢复结构，不恢复曾经的覆盖值。
ALTER TABLE tenant_quotas ADD COLUMN daily_token_refresh INTEGER;
ALTER TABLE plans ADD COLUMN daily_token_refresh INTEGER NOT NULL DEFAULT 5000;
