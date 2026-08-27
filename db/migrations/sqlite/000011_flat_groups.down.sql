-- migrate:no-transaction
--
-- 只恢复结构，不恢复层级：parent_id 全为 NULL、level 全为 1，
-- 也就是所有分组都是顶级分组。原来的父子关系在 up 里已经不存在了。
PRAGMA foreign_keys = OFF;

BEGIN;

DROP TABLE IF EXISTS mail_groups_old;

CREATE TABLE mail_groups_old (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_id            TEXT REFERENCES mail_groups_old(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    color                TEXT NOT NULL DEFAULT 'gray'
                         CHECK (color IN ('blue', 'green', 'amber', 'red', 'purple', 'gray')),
    level                INTEGER NOT NULL DEFAULT 1 CHECK (level IN (1, 2, 3)),
    sort_order           INTEGER NOT NULL DEFAULT 0,
    is_system            INTEGER NOT NULL DEFAULT 0,
    proxy_url            TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_1 TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_2 TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, name)
);

INSERT INTO mail_groups_old (
    id, tenant_id, name, description, color, sort_order, is_system,
    proxy_url, fallback_proxy_url_1, fallback_proxy_url_2, created_at, updated_at
)
SELECT id, tenant_id, name, description, color, sort_order, is_system,
       proxy_url, fallback_proxy_url_1, fallback_proxy_url_2, created_at, updated_at
FROM mail_groups;

DROP TABLE mail_groups;
ALTER TABLE mail_groups_old RENAME TO mail_groups;

CREATE INDEX IF NOT EXISTS idx_mail_groups_tenant_parent ON mail_groups(tenant_id, parent_id, sort_order);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_groups_system ON mail_groups(tenant_id) WHERE is_system = 1;

COMMIT;

PRAGMA foreign_key_check;

PRAGMA foreign_keys = ON;
