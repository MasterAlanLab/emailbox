DROP INDEX IF EXISTS idx_mail_groups_next_refresh;
ALTER TABLE mail_groups DROP COLUMN IF EXISTS next_refresh_at;
ALTER TABLE mail_groups DROP COLUMN IF EXISTS refresh_interval_minutes;
