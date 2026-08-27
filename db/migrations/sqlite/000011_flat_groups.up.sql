-- migrate:no-transaction
--
-- 分组从「最多三级的树」改成一层的列表。
--
-- 树是照着 outlookEmail 的老界面搬过来的，但它在这个产品里没有对应的用法：
-- 用户要的是「把账号分堆」，而层级带来的东西全是负担——建分组时要先选上级、
-- 界面上要解释「最多三层」、账号数要分「直属 / 含子树」两个口径、删除要交代
-- 子分组会一起消失。功能没多，规则先多了一圈。
--
-- 原来的子分组一律变成顶级分组：账号还挂在原来的分组上（group_id 不动），
-- 只是分组之间不再有从属关系。UNIQUE(tenant_id, name) 本来就是全租户唯一，
-- 压平不会撞名。分组自己的代理配置在 000010 已经下推过，此处不再涉及。
--
-- SQLite 去不掉列上的外键（parent_id 自引用）和 CHECK（level IN (1,2,3)），
-- 只能整表重建；重建要 DROP 旧表，而 mail_accounts.group_id 指着它，
-- 因此要先关外键检查——那个 PRAGMA 在事务内是空操作，只能用 no-transaction
-- 自己管事务（同 000008）。
--
-- 该指令要求文件可重复执行：这里只搬「压平后还存在」的列，重跑时 mail_groups
-- 已是新结构，照样能原样再重建一遍。
PRAGMA foreign_keys = OFF;

BEGIN;

-- 上一次跑到一半崩在这里的话，mail_groups_new 会留下来挡住 CREATE。
DROP TABLE IF EXISTS mail_groups_new;

CREATE TABLE mail_groups_new (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    -- 存 Kumo 的语义令牌名而非十六进制色值，前端映射到 Badge 的 variant
    color                TEXT NOT NULL DEFAULT 'gray'
                         CHECK (color IN ('blue', 'green', 'amber', 'red', 'purple', 'gray')),
    sort_order           INTEGER NOT NULL DEFAULT 0,
    -- 每个租户有且仅有一个 is_system=1 的默认分组，删除其它分组时账号回落到它
    is_system            INTEGER NOT NULL DEFAULT 0,
    -- 代理三列构成「主 + 两个备用」，账号没配时用所属分组的
    proxy_url            TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_1 TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_2 TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, name)
);

-- 显式列出列名，不用 SELECT *：靠位置对齐的话，以后任何一次 ALTER 都会让
-- 这条 INSERT 静默串列。
INSERT INTO mail_groups_new (
    id, tenant_id, name, description, color, sort_order, is_system,
    proxy_url, fallback_proxy_url_1, fallback_proxy_url_2, created_at, updated_at
)
SELECT id, tenant_id, name, description, color, sort_order, is_system,
       proxy_url, fallback_proxy_url_1, fallback_proxy_url_2, created_at, updated_at
FROM mail_groups;

DROP TABLE mail_groups;
ALTER TABLE mail_groups_new RENAME TO mail_groups;

-- idx_mail_groups_tenant_parent 随旧表消失。列表按 tenant_id + sort_order 取，
-- 索引跟着改成这两列。
CREATE INDEX IF NOT EXISTS idx_mail_groups_tenant_sort ON mail_groups(tenant_id, sort_order);
CREATE UNIQUE INDEX IF NOT EXISTS idx_mail_groups_system ON mail_groups(tenant_id) WHERE is_system = 1;

COMMIT;

-- 重建后再核一次：mail_accounts.group_id 是否仍然全部指得到分组。
PRAGMA foreign_key_check;

PRAGMA foreign_keys = ON;
