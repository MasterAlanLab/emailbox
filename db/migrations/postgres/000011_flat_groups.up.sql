-- 分组从「最多三级的树」改成一层的列表，理由见 sqlite/000011_flat_groups.up.sql。
--
-- PostgreSQL 能直接删列，不必像 SQLite 那样整表重建：ON DELETE CASCADE 的自引用
-- 外键与 level 的 CHECK 都随列一起消失。原来的子分组一律变成顶级分组，
-- 账号仍挂在原来的分组上。
DROP INDEX IF EXISTS idx_mail_groups_tenant_parent;

ALTER TABLE mail_groups DROP COLUMN IF EXISTS parent_id;
ALTER TABLE mail_groups DROP COLUMN IF EXISTS level;

-- 列表按 tenant_id + sort_order 取。
CREATE INDEX IF NOT EXISTS idx_mail_groups_tenant_sort ON mail_groups(tenant_id, sort_order);
