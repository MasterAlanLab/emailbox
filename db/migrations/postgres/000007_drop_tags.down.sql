-- 只恢复结构，不恢复数据。DDL 与 000003_mail_core 里的原始定义一致。
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
