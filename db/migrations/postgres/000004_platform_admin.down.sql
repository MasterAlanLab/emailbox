ALTER TABLE users DROP COLUMN last_login_at;
DROP INDEX IF EXISTS idx_audit_logs_created;
DROP INDEX IF EXISTS idx_audit_logs_actor;
DROP INDEX IF EXISTS idx_audit_logs_tenant;
DROP TABLE IF EXISTS audit_logs;
