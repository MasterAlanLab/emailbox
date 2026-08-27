-- 取消「每日刷新令牌」的额度，理由见 sqlite/000013_drop_token_refresh_quota.up.sql。
ALTER TABLE plans DROP COLUMN IF EXISTS daily_token_refresh;
ALTER TABLE tenant_quotas DROP COLUMN IF EXISTS daily_token_refresh;
