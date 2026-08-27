-- 任务系统与刷新日志。设计见 docs/plan/03-data-model.md §4.1、§4.2 与 02-architecture.md §4。
--
-- 任务状态必须入库而不是留在进程内存里：批量刷新 5000 个账号要跑几分钟，
-- 这期间浏览器会断线、用户会刷新页面、服务可能重启。
-- outlookEmail 把进度放内存，因此被迫单进程部署且刷新页面即丢失进度。

CREATE TABLE jobs (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    type           TEXT NOT NULL,
    trigger        TEXT NOT NULL DEFAULT 'manual',
    status         TEXT NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending','running','stopping','succeeded','partial','failed','stopped','interrupted')),
    created_by     TEXT REFERENCES users(id),
    total_count    INTEGER NOT NULL DEFAULT 0,
    success_count  INTEGER NOT NULL DEFAULT 0,
    failed_count   INTEGER NOT NULL DEFAULT 0,
    params         TEXT NOT NULL DEFAULT '{}',
    error_summary  TEXT NOT NULL DEFAULT '',
    started_at     DATETIME,
    finished_at    DATETIME,
    -- heartbeat_at 每几秒更新一次。进程被强杀时没人来改 status，
    -- 靠它在下次启动时把僵尸任务识别出来标成 interrupted。
    heartbeat_at   DATETIME,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_jobs_tenant_type ON jobs(tenant_id, type, created_at DESC);

-- 启动时扫僵尸任务只关心还没终结的那几条，部分索引足够小
CREATE INDEX idx_jobs_running ON jobs(status, heartbeat_at) WHERE status IN ('running','stopping');

CREATE TABLE job_items (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    -- 账号被删之后置空而不是级联删除：任务历史要能回看「当时处理了什么」。
    account_id  TEXT REFERENCES mail_accounts(id) ON DELETE SET NULL,
    -- email 是快照。上面那个外键置空后，只有这一列还能说明这行是哪个邮箱。
    email       TEXT NOT NULL DEFAULT '',
    -- position 是提交时的序号。不能靠 rowid 排序（PostgreSQL 没有这个概念），
    -- 也不能靠 id 排序（UUID 是随机的）——而 worker 的取件顺序和列表分页
    -- 都需要一个稳定且两个引擎一致的次序。
    position    INTEGER NOT NULL DEFAULT 0,
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','running','success','failed','skipped')),
    error_kind  TEXT NOT NULL DEFAULT '',
    error       TEXT NOT NULL DEFAULT '',
    started_at  DATETIME,
    finished_at DATETIME
);

CREATE INDEX idx_job_items_job ON job_items(job_id, status);

-- job_events 是 SSE 断线重连的关键：前端带 Last-Event-ID 重连，
-- 服务端 WHERE job_id = ? AND seq > ? 回放即可。
CREATE TABLE job_events (
    id         TEXT PRIMARY KEY,
    job_id     TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    kind       TEXT NOT NULL,
    payload    TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (job_id, seq)
);

CREATE TABLE mail_refresh_logs (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id    TEXT REFERENCES mail_accounts(id) ON DELETE CASCADE,
    account_email TEXT NOT NULL,
    job_id        TEXT REFERENCES jobs(id) ON DELETE SET NULL,
    refresh_type  TEXT NOT NULL DEFAULT 'manual',
    status        TEXT NOT NULL CHECK (status IN ('success','failed')),
    error_kind    TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_refresh_logs_tenant  ON mail_refresh_logs(tenant_id, created_at DESC);
CREATE INDEX idx_refresh_logs_account ON mail_refresh_logs(account_id, created_at DESC);

-- 「最近失败了哪些」是这张表最高频的查法，单独给一条索引
CREATE INDEX idx_refresh_logs_failed ON mail_refresh_logs(tenant_id, status, created_at DESC);
