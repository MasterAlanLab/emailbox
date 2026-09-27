DROP INDEX IF EXISTS idx_mail_accounts_health;
ALTER TABLE mail_accounts DROP COLUMN health_checked_at;
ALTER TABLE mail_accounts DROP COLUMN health_error;
ALTER TABLE mail_accounts DROP COLUMN health_error_kind;
ALTER TABLE mail_accounts DROP COLUMN health_status;
