-- SaaS 形态：平台角色、个人工作空间、套餐与配额。
-- 设计见 docs/plan/08-saas-admin.md。
-- 布尔类字段沿用 INTEGER，与 SQLite 侧保持同一种 Go 类型，减少 repo 层的方言差异。

-- 平台角色与租户角色是两个正交维度：
-- tenant_members.role 决定「在某个租户内能做什么」，platform_role 决定「能否跨租户管理整个系统」。
ALTER TABLE users ADD COLUMN platform_role VARCHAR(20) NOT NULL DEFAULT 'user'
    CHECK (platform_role IN ('user', 'admin'));

-- 管理员是极少数，部分索引只收录他们
CREATE INDEX idx_users_platform_role ON users(platform_role) WHERE platform_role = 'admin';

ALTER TABLE tenants ADD COLUMN kind VARCHAR(20) NOT NULL DEFAULT 'personal'
    CHECK (kind IN ('personal', 'team'));

-- 本迁移之前创建的租户属于旧的「多工作空间」模型，不满足「每人至多一个个人空间」的约束，
-- 因此统一归为 team；个人空间由本迁移之后的注册流程创建。
UPDATE tenants SET kind = 'team';

-- 一个用户有且只有一个个人工作空间。删掉自己的个人空间会让账号变成无处可去的孤儿，
-- 因此 service 层还会禁止删除 kind='personal' 的租户。
CREATE UNIQUE INDEX idx_tenants_personal_owner ON tenants(created_by) WHERE kind = 'personal';

-- 套餐：配额的基线值。-1 表示不限。
CREATE TABLE plans (
    id                  TEXT PRIMARY KEY,
    code                VARCHAR(50) NOT NULL UNIQUE,
    name                VARCHAR(100) NOT NULL,
    is_default          INTEGER NOT NULL DEFAULT 0,
    max_accounts        INTEGER NOT NULL DEFAULT 50,
    max_groups          INTEGER NOT NULL DEFAULT 20,
    max_api_keys        INTEGER NOT NULL DEFAULT 2,
    max_share_links     INTEGER NOT NULL DEFAULT 10,
    daily_mail_fetch    INTEGER NOT NULL DEFAULT 2000,
    daily_token_refresh INTEGER NOT NULL DEFAULT 5000,
    allow_forwarding    INTEGER NOT NULL DEFAULT 0,
    allow_retention     INTEGER NOT NULL DEFAULT 0,
    allow_external_api  INTEGER NOT NULL DEFAULT 0,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 开放注册平台的默认套餐给保守额度，避免被薅
INSERT INTO plans (id, code, name, is_default) VALUES
    ('00000000-0000-4000-8000-000000000001', 'free', '免费版', 1);

-- 租户的生效配额 = 所属套餐 + 管理员针对该租户的覆盖值（NULL 表示不覆盖）
CREATE TABLE tenant_quotas (
    tenant_id           TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    plan_id             TEXT NOT NULL REFERENCES plans(id),
    max_accounts        INTEGER,
    max_groups          INTEGER,
    max_api_keys        INTEGER,
    max_share_links     INTEGER,
    daily_mail_fetch    INTEGER,
    daily_token_refresh INTEGER,
    allow_forwarding    INTEGER,
    allow_retention     INTEGER,
    allow_external_api  INTEGER,
    note                TEXT NOT NULL DEFAULT '',
    updated_by          TEXT REFERENCES users(id),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 按天累加的用量计数，供 daily_* 类配额使用
CREATE TABLE usage_counters (
    tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day       VARCHAR(10) NOT NULL,
    metric    VARCHAR(50) NOT NULL,
    count     INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day, metric)
);

CREATE INDEX idx_usage_counters_day ON usage_counters(day);
