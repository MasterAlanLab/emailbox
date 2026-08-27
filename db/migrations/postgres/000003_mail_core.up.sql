-- 邮箱业务的核心表：分组树、账号、别名、标签。
-- 设计见 docs/plan/03-data-model.md §3。
--
-- 隔离不变量：所有业务表带 tenant_id，且每一条查询的 WHERE 都必须带上它。
-- 管理员的跨租户访问也不放宽这条规则，只是由 URL 显式指定 tenant_id。

-- 分组树，最多三级。level 由 service 在创建/移动时计算并校验。
CREATE TABLE mail_groups (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_id            TEXT REFERENCES mail_groups(id) ON DELETE CASCADE,
    name                 VARCHAR(100) NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    -- 存 Kumo 的语义令牌名而非十六进制色值，前端映射到 Badge 的 variant
    color                VARCHAR(20) NOT NULL DEFAULT 'gray'
                         CHECK (color IN ('blue', 'green', 'amber', 'red', 'purple', 'gray')),
    level                INTEGER NOT NULL DEFAULT 1 CHECK (level IN (1, 2, 3)),
    sort_order           INTEGER NOT NULL DEFAULT 0,
    -- 每个租户有且仅有一个 is_system=1 的默认分组，删除其它分组时账号回落到它
    is_system            INTEGER NOT NULL DEFAULT 0,
    -- 代理三列构成「主 + 两个备用」，子分组为空时向上继承
    proxy_url            TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_1 TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_2 TEXT NOT NULL DEFAULT '',
    created_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, name)
);

CREATE INDEX idx_mail_groups_tenant_parent ON mail_groups(tenant_id, parent_id, sort_order);

-- 每个租户至多一个系统默认分组
CREATE UNIQUE INDEX idx_mail_groups_system ON mail_groups(tenant_id) WHERE is_system = 1;

CREATE TABLE mail_accounts (
    id                       TEXT PRIMARY KEY,
    tenant_id                TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    group_id                 TEXT NOT NULL REFERENCES mail_groups(id),
    email                    VARCHAR(255) NOT NULL,
    -- 唯一索引建在 normalized 上：COLLATE NOCASE 在 PostgreSQL 上不成立
    email_normalized         VARCHAR(255) NOT NULL,
    provider                 VARCHAR(50) NOT NULL DEFAULT 'outlook',
    account_type             VARCHAR(20) NOT NULL DEFAULT 'outlook'
                             CHECK (account_type IN ('outlook', 'imap')),
    -- 最近成功的通道。存到具体通道（而非只存 graph|imap）可直接命中上次成功的服务器
    auth_channel             VARCHAR(20) NOT NULL DEFAULT ''
                             CHECK (auth_channel IN ('', 'graph', 'imap_new', 'imap_old')),
    password_enc             TEXT NOT NULL DEFAULT '',
    client_id                VARCHAR(100) NOT NULL DEFAULT '',
    refresh_token_enc        TEXT NOT NULL DEFAULT '',
    imap_host                VARCHAR(255) NOT NULL DEFAULT '',
    imap_port                INTEGER NOT NULL DEFAULT 993,
    imap_password_enc        TEXT NOT NULL DEFAULT '',
    status                   VARCHAR(20) NOT NULL DEFAULT 'active'
                             CHECK (status IN ('active', 'disabled', 'banned')),
    remark                   TEXT NOT NULL DEFAULT '',
    sort_order               INTEGER NOT NULL DEFAULT 0,
    proxy_url                TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_1     TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_2     TEXT NOT NULL DEFAULT '',
    forward_enabled          INTEGER NOT NULL DEFAULT 0,
    forward_cursor           TEXT NOT NULL DEFAULT '',
    forward_last_checked_at  TIMESTAMPTZ,
    last_refresh_at          TIMESTAMPTZ,
    last_refresh_status      VARCHAR(20) NOT NULL DEFAULT 'never'
                             CHECK (last_refresh_status IN ('never', 'success', 'failed')),
    last_refresh_error       TEXT NOT NULL DEFAULT '',
    refresh_token_updated_at TIMESTAMPTZ,
    created_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- 软删除只对「误删可恢复」有意义；软删时会立即清空三个密文列，
    -- 凭据密文不该跟着软删除长期留存
    deleted_at               TIMESTAMPTZ
);

CREATE UNIQUE INDEX idx_mail_accounts_tenant_email ON mail_accounts(tenant_id, email_normalized)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_mail_accounts_group ON mail_accounts(tenant_id, group_id, sort_order);
CREATE INDEX idx_mail_accounts_refresh ON mail_accounts(tenant_id, last_refresh_status, last_refresh_at);
CREATE INDEX idx_mail_accounts_forward ON mail_accounts(tenant_id, forward_enabled) WHERE forward_enabled = 1;
CREATE INDEX idx_mail_accounts_status ON mail_accounts(tenant_id, status);

CREATE TABLE mail_account_aliases (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id       TEXT NOT NULL REFERENCES mail_accounts(id) ON DELETE CASCADE,
    alias_email      VARCHAR(255) NOT NULL,
    alias_normalized VARCHAR(255) NOT NULL,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, alias_normalized)
);

CREATE INDEX idx_mail_aliases_account ON mail_account_aliases(account_id);

CREATE TABLE mail_tags (
    id         TEXT PRIMARY KEY,
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    name       VARCHAR(100) NOT NULL,
    color      VARCHAR(20) NOT NULL DEFAULT 'blue'
               CHECK (color IN ('blue', 'green', 'amber', 'red', 'purple', 'gray')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, name)
);

CREATE TABLE mail_account_tags (
    account_id TEXT NOT NULL REFERENCES mail_accounts(id) ON DELETE CASCADE,
    tag_id     TEXT NOT NULL REFERENCES mail_tags(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (account_id, tag_id)
);

CREATE INDEX idx_mail_account_tags_tag ON mail_account_tags(tag_id);
