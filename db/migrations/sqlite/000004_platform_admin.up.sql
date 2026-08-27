-- 审计日志。设计见 docs/plan/03-data-model.md §4.3 与 08-saas-admin.md §2.4。
--
-- 03 文档原本把它和 P4 的 jobs / job_items / job_events / refresh_logs 一起放进
-- 000004_mail_ops，但审计是 P3 管理后台的硬依赖（管理员跨租户的每一次读写都要留痕），
-- 而任务系统不是。捆在一起会让 P3 平白引入四张本期不用的空表，因此拆开单独落。
CREATE TABLE audit_logs (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    -- 操作者被删除后日志必须留下：置空指向，而不是级联删掉这条记录。
    -- 审计日志会因为「操作者被删」而消失的话，它就挡不住任何真正需要它的场景。
    actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    -- 冗余存一份操作者邮箱：上面那个外键被置空之后，光看 NULL 无从追溯是谁。
    actor_email   TEXT NOT NULL DEFAULT '',
    actor_kind    TEXT NOT NULL DEFAULT 'user'
        CHECK (actor_kind IN ('user', 'admin', 'api_key', 'system')),
    action        TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    ip            TEXT NOT NULL DEFAULT '',
    details       TEXT NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 后台按租户翻日志是最常见的查法
CREATE INDEX idx_audit_logs_tenant ON audit_logs(tenant_id, created_at DESC);

-- 「这个管理员做过什么」是追责时的第一个问题，也是管理员自查页面的查法
CREATE INDEX idx_audit_logs_actor ON audit_logs(actor_user_id, created_at DESC);

-- 全平台按时间倒序翻页（/admin/audit 的默认视图）。
-- 前两个索引都以别的列打头，帮不上这个查询。
CREATE INDEX idx_audit_logs_created ON audit_logs(created_at DESC);

-- 后台用户列表要显示「最后登录时间」（05 文档 §12.1）。
-- 不用 MAX(sessions.created_at) 反推：退出登录会删掉 session 行，
-- 那样一个刚登录又退出的用户会显示成「从未登录」，恰好是最需要看清的那类账号。
ALTER TABLE users ADD COLUMN last_login_at DATETIME;
