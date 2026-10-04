-- 按产品决定移除审计日志功能，同时删除旧表、索引与历史记录。
-- 保留历史迁移，使已部署数据库与全新数据库沿同一条升级链到达最新结构。
DROP INDEX IF EXISTS idx_audit_logs_created;
DROP INDEX IF EXISTS idx_audit_logs_actor;
DROP INDEX IF EXISTS idx_audit_logs_tenant;
DROP TABLE IF EXISTS audit_logs;
