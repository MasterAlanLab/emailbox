DROP INDEX IF EXISTS idx_usage_counters_day;
DROP TABLE IF EXISTS usage_counters;
DROP TABLE IF EXISTS tenant_quotas;
DROP TABLE IF EXISTS plans;
DROP INDEX IF EXISTS idx_tenants_personal_owner;
ALTER TABLE tenants DROP COLUMN kind;
DROP INDEX IF EXISTS idx_users_platform_role;
ALTER TABLE users DROP COLUMN platform_role;
