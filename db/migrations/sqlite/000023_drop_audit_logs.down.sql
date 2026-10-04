-- 只恢复表结构（即 000004 + 000009 之后的形状），删掉的记录回不来。
CREATE TABLE audit_logs (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    actor_name    TEXT NOT NULL DEFAULT '',
    actor_kind    TEXT NOT NULL DEFAULT 'user'
        CHECK (actor_kind IN ('user', 'admin', 'api_key', 'system')),
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    ip            TEXT NOT NULL DEFAULT '',
    details       TEXT NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_audit_logs_tenant ON audit_logs(tenant_id, created_at DESC);
CREATE INDEX idx_audit_logs_actor ON audit_logs(actor_user_id, created_at DESC);
CREATE INDEX idx_audit_logs_created ON audit_logs(created_at DESC);
