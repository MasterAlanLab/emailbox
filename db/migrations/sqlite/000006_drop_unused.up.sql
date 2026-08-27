-- 清理为已删除范围（转发与调度、对外 API / 分享链接 / 本地邮件保留）预埋的列。
-- 这些能力已于 2026-08-21 从方案中移除，见 docs/plan/07-roadmap.md §5。
--
-- 留着它们的代价不只是几列空间：套餐表单里那三个 allow_* 开关管理员能开，
-- 用量页还把它们当作套餐权益展示给用户——一个开了之后什么都不会发生的开关，
-- 比没有这个开关更糟。

-- SQLite 不允许 DROP 一个仍被索引引用的列，先拆索引。
DROP INDEX IF EXISTS idx_mail_accounts_forward;

ALTER TABLE mail_accounts DROP COLUMN forward_enabled;
ALTER TABLE mail_accounts DROP COLUMN forward_cursor;
ALTER TABLE mail_accounts DROP COLUMN forward_last_checked_at;

ALTER TABLE plans DROP COLUMN max_api_keys;
ALTER TABLE plans DROP COLUMN max_share_links;
ALTER TABLE plans DROP COLUMN allow_forwarding;
ALTER TABLE plans DROP COLUMN allow_retention;
ALTER TABLE plans DROP COLUMN allow_external_api;

ALTER TABLE tenant_quotas DROP COLUMN max_api_keys;
ALTER TABLE tenant_quotas DROP COLUMN max_share_links;
ALTER TABLE tenant_quotas DROP COLUMN allow_forwarding;
ALTER TABLE tenant_quotas DROP COLUMN allow_retention;
ALTER TABLE tenant_quotas DROP COLUMN allow_external_api;
