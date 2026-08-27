-- 只恢复结构，不恢复层级：parent_id 全为 NULL、level 全为 1。
DROP INDEX IF EXISTS idx_mail_groups_tenant_sort;

ALTER TABLE mail_groups ADD COLUMN parent_id TEXT REFERENCES mail_groups(id) ON DELETE CASCADE;
ALTER TABLE mail_groups ADD COLUMN level INTEGER NOT NULL DEFAULT 1 CHECK (level IN (1, 2, 3));

CREATE INDEX IF NOT EXISTS idx_mail_groups_tenant_parent ON mail_groups(tenant_id, parent_id, sort_order);
