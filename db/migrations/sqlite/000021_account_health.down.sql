-- 索引必须先删：SQLite 拒绝 DROP 掉一个被索引引用的列。
DROP INDEX IF EXISTS idx_mail_accounts_health;
ALTER TABLE mail_accounts DROP COLUMN health_checked_at;
ALTER TABLE mail_accounts DROP COLUMN health_error;
ALTER TABLE mail_accounts DROP COLUMN health_error_kind;
ALTER TABLE mail_accounts DROP COLUMN health_status;
