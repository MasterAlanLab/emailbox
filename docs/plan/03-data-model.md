# 03 · 数据模型设计

## 1. 总体约定

- **主键**：沿用模板风格，`TEXT PRIMARY KEY` 存 UUID（`uuid.NewString()`）。
  outlookEmail 用自增整数，但模板的租户/用户全是 UUID，混用会让外键与前端类型都变脏。
- **租户隔离**：所有业务表带 `tenant_id TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE`。
  **每一条查询的 WHERE 都必须带 tenant_id**——这是本项目最重要的安全不变量，
  在 `db/query/` 的 SQL 里强制体现，而不是靠 service 层记得传。
  **管理员的跨租户访问也不放宽这条规则**：管理员 API 显式指定目标 `tenant_id`，
  走完全相同的查询路径，详见 [08 文档 §2.3](08-saas-admin.md)。
- **SaaS 形态**：每个注册用户自动获得一个 `kind='personal'` 的个人租户（工作空间），
  邮箱资源归属于该租户，用户间天然隔离。租户层对普通用户在 UI 上隐藏。
  平台管理员由 `users.platform_role` 标识，与租户角色正交。完整设计见 [08 文档](08-saas-admin.md)。
- **软删除**：只有 `mail_accounts` 需要（误删上万账号不可逆），用 `deleted_at`；
  其余表硬删除。
- **时间**：`DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP`（sqlite）/ `TIMESTAMPTZ`（postgres），
  与模板 `000001_init` 保持一致。
- **邮箱规范化**：所有邮箱同时存 `email`（原样）与 `email_normalized`（`strings.ToLower(TrimSpace)`），
  唯一索引建在 normalized 上。outlookEmail 靠 `COLLATE NOCASE` 索引解决，在 postgres 上不成立。

## 2. 迁移划分

| 迁移 | 内容 | 阶段 |
|---|---|---|
| `000002_saas` | `users.platform_role`、`tenants.kind`、plans / tenant_quotas / usage_counters | P0 |
| `000003_mail_core` | groups / accounts / aliases / tags / account_tags | P1 |
| `000004_platform_admin` | audit_logs、`users.last_login_at` | P3 |
| `000005_mail_ops` | jobs / job_items / job_events / refresh_logs | P4 |

`000005_mail_ops` 是最后一个迁移——转发、对外 API、分享链接、本地邮件保留、临时邮箱
相关的表已随 P5–P7 一起从方案中删除（[07 文档 §5](07-roadmap.md)）。

每个迁移 sqlite / postgres 各一份 `.up.sql` / `.down.sql`。
`000002_saas` 的表定义见 [08 文档 §2、§4](08-saas-admin.md)，不在本文重复。

## 3. 核心表

### 3.1 `mail_groups` — 分组树（最多三级）

```sql
CREATE TABLE mail_groups (
    id                   TEXT PRIMARY KEY,
    tenant_id            TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    parent_id            TEXT REFERENCES mail_groups(id) ON DELETE CASCADE,
    name                 TEXT NOT NULL,
    description          TEXT NOT NULL DEFAULT '',
    color                TEXT NOT NULL DEFAULT 'gray',  -- 语义令牌名，见 06 文档 §1.4
    level                INTEGER NOT NULL DEFAULT 1 CHECK (level IN (1,2,3)),
    sort_order           INTEGER NOT NULL DEFAULT 0,
    is_system            INTEGER NOT NULL DEFAULT 0,
    proxy_url            TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_1 TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_2 TEXT NOT NULL DEFAULT '',
    created_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at           DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, name)
);
CREATE INDEX idx_mail_groups_tenant_parent ON mail_groups(tenant_id, parent_id, sort_order);
```

- `level` 由 service 在创建/移动时计算并校验（≤3），移动子树时递归重算——
  对应 Python 的 `rebuild_group_levels` / `validate_group_move`（含防环检测）。
- 每个租户在首次使用时自动创建一个 `is_system=1` 的「默认分组」；删除分组时，
  其下账号回落到默认分组，子分组级联删除（与 outlookEmail 行为一致）。
- 代理三列共同构成「主 + 两个备用」，子分组为空时向上继承（见 04 文档）。

### 3.2 `mail_accounts` — 邮箱账号（核心表）

```sql
CREATE TABLE mail_accounts (
    id                       TEXT PRIMARY KEY,
    tenant_id                TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    group_id                 TEXT NOT NULL REFERENCES mail_groups(id),
    email                    TEXT NOT NULL,
    email_normalized         TEXT NOT NULL,
    provider                 TEXT NOT NULL DEFAULT 'outlook',  -- outlook/gmail/qq/163/126/yahoo/aliyun/2925/custom
    account_type             TEXT NOT NULL DEFAULT 'outlook',  -- outlook | imap
    auth_channel             TEXT NOT NULL DEFAULT '',         -- '' | graph | imap_new | imap_old  最近成功通道
    password_enc             TEXT NOT NULL DEFAULT '',
    client_id                TEXT NOT NULL DEFAULT '',
    refresh_token_enc        TEXT NOT NULL DEFAULT '',
    imap_host                TEXT NOT NULL DEFAULT '',
    imap_port                INTEGER NOT NULL DEFAULT 993,
    imap_password_enc        TEXT NOT NULL DEFAULT '',
    status                   TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active','disabled','banned')),
    remark                   TEXT NOT NULL DEFAULT '',
    sort_order               INTEGER NOT NULL DEFAULT 0,
    proxy_url                TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_1     TEXT NOT NULL DEFAULT '',
    fallback_proxy_url_2     TEXT NOT NULL DEFAULT '',
    last_refresh_at          DATETIME,
    last_refresh_status      TEXT NOT NULL DEFAULT 'never' CHECK (last_refresh_status IN ('never','success','failed')),
    last_refresh_error       TEXT NOT NULL DEFAULT '',
    refresh_token_updated_at DATETIME,
    created_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at               DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at               DATETIME
);

CREATE UNIQUE INDEX idx_mail_accounts_tenant_email ON mail_accounts(tenant_id, email_normalized)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_mail_accounts_group      ON mail_accounts(tenant_id, group_id, sort_order);
CREATE INDEX idx_mail_accounts_refresh    ON mail_accounts(tenant_id, last_refresh_status, last_refresh_at);
CREATE INDEX idx_mail_accounts_status     ON mail_accounts(tenant_id, status);
```

> **`000003` 里曾有三个 `forward_*` 列与 `idx_mail_accounts_forward` 索引**，
> 是转发功能被移出方案（[07 文档 §5](07-roadmap.md)）后留下的遗留物，
> 已由 `000006_drop_unused` 连同账号筛选里的 `forward` 条件一并删除。
> 上面的建表语句是清理后的最终形态。

> **`auth_channel` 的取值比 outlookEmail 更细。** Python 只存 `graph|imap`，
> 无法区分 IMAP 新旧服务器，回退链每次都要从 `outlook.live.com` 重试。
> 这里存到具体通道，可直接命中上次成功的服务器。

**关于部分唯一索引**：SQLite 3.8+ 与 PostgreSQL 都支持 `CREATE UNIQUE INDEX ... WHERE`，
两套迁移写法一致。若目标 SQLite 版本过老，退化为普通唯一索引 + 删除时物理删除。

### 3.3 `mail_account_aliases` — 别名

```sql
CREATE TABLE mail_account_aliases (
    id               TEXT PRIMARY KEY,
    tenant_id        TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    account_id       TEXT NOT NULL REFERENCES mail_accounts(id) ON DELETE CASCADE,
    alias_email      TEXT NOT NULL,
    alias_normalized TEXT NOT NULL,
    created_at       DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (tenant_id, alias_normalized)
);
CREATE INDEX idx_mail_aliases_account ON mail_account_aliases(account_id);
```

校验规则（照搬 outlookEmail 的 `validate_account_aliases`）：别名不能与任何账号的
主邮箱冲突、不能与其它账号的别名冲突、不能等于自己的主邮箱。

> **`000003` 里曾有 `mail_tags` / `mail_account_tags` 两张表**，已由
> [`000007_drop_tags`](../../db/migrations/sqlite/000007_drop_tags.up.sql) 删除（2026-08-27）。
> 标签这个功能在界面上从来没有闭合过：`/mail/tags` 能建能删，账号列表也会显示
> `account.tags`，但**没有任何地方能把标签贴到账号上**——`batchTags` 接口没有调用方，
> 导入页也不传 `tag_ids`。于是账号上的标签永远是空的。分组承担了「给账号分类」
> 这件事，且它有完整的树结构与配额，没有必要再留一套只写不读的平行机制。
>
> 颜色令牌本身**保留**：分组的 `color` 列用的就是同一套取值（见 §3.1）。

## 4. 运维表

### 4.1 `jobs` / `job_items` / `job_events`

```sql
CREATE TABLE jobs (
    id             TEXT PRIMARY KEY,
    tenant_id      TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    type           TEXT NOT NULL,   -- token_refresh | import | export
    trigger        TEXT NOT NULL DEFAULT 'manual',  -- manual | scheduled
    status         TEXT NOT NULL DEFAULT 'pending'
                   CHECK (status IN ('pending','running','stopping','succeeded','partial','failed','stopped','interrupted')),
    created_by     TEXT REFERENCES users(id),
    total_count    INTEGER NOT NULL DEFAULT 0,
    success_count  INTEGER NOT NULL DEFAULT 0,
    failed_count   INTEGER NOT NULL DEFAULT 0,
    params         TEXT NOT NULL DEFAULT '{}',   -- JSON
    error_summary  TEXT NOT NULL DEFAULT '',
    started_at     DATETIME,
    finished_at    DATETIME,
    heartbeat_at   DATETIME,
    created_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_jobs_tenant_type ON jobs(tenant_id, type, created_at DESC);
CREATE INDEX idx_jobs_running     ON jobs(status, heartbeat_at) WHERE status IN ('running','stopping');

CREATE TABLE job_items (
    id          TEXT PRIMARY KEY,
    job_id      TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    account_id  TEXT REFERENCES mail_accounts(id) ON DELETE SET NULL,
    email       TEXT NOT NULL DEFAULT '',   -- 快照，账号删了仍可看历史
    status      TEXT NOT NULL DEFAULT 'pending'
                CHECK (status IN ('pending','running','success','failed','skipped')),
    error_kind  TEXT NOT NULL DEFAULT '',   -- auth_failed | banned | proxy_failed | network | provider_error
    error       TEXT NOT NULL DEFAULT '',
    started_at  DATETIME,
    finished_at DATETIME
);
CREATE INDEX idx_job_items_job ON job_items(job_id, status);

CREATE TABLE job_events (
    id       TEXT PRIMARY KEY,
    job_id   TEXT NOT NULL REFERENCES jobs(id) ON DELETE CASCADE,
    seq      INTEGER NOT NULL,          -- 单调递增，SSE Last-Event-ID 用
    kind     TEXT NOT NULL,             -- started | progress | item | finished | error
    payload  TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (job_id, seq)
);
```

> `job_events` 是 SSE 断线重连的关键：前端带 `Last-Event-ID` 重连，服务端
> `WHERE job_id=? AND seq > ?` 回放即可。这是 outlookEmail 无法做到的
> （它的进度只在进程内存里，刷新页面即丢失）。
> 保留期由 `JOB_EVENT_RETENTION` 控制，定时清理。

### 4.2 `mail_refresh_logs`

```sql
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
CREATE INDEX idx_refresh_logs_failed  ON mail_refresh_logs(tenant_id, status, created_at DESC);
```

### 4.3 `audit_logs`

```sql
CREATE TABLE audit_logs (
    id            TEXT PRIMARY KEY,
    tenant_id     TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    actor_user_id TEXT REFERENCES users(id) ON DELETE SET NULL,
    actor_kind    TEXT NOT NULL DEFAULT 'user',   -- user | admin | system（CHECK 里还留着已废弃的 api_key）
    action        TEXT NOT NULL,                  -- account.import / account.delete / plan.update / ...
    resource_type TEXT NOT NULL,
    resource_id   TEXT NOT NULL DEFAULT '',
    ip            TEXT NOT NULL DEFAULT '',
    details       TEXT NOT NULL DEFAULT '{}',
    created_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX idx_audit_logs_tenant ON audit_logs(tenant_id, created_at DESC);
```

## 5. sqlc 组织

`db/query/{sqlite,postgres}/` 下按功能域拆文件，两边文件名一一对应：

```
mail_groups.sql      mail_accounts.sql    mail_aliases.sql
jobs.sql             mail_refresh_logs.sql
audit_logs.sql
```

`sqlc.yaml` 无需改动（`queries` 指的是目录）。但 `schema` 目前只指向 `000001_init.up.sql`，
**新增迁移后必须把 schema 改为目录**：

```yaml
schema: "db/migrations/sqlite"     # 而非单个文件
```

sqlc 会按文件名排序读取，这要求迁移文件名保持 `000002_`、`000003_` 的零填充前缀。
（`.down.sql` 会被 sqlc 自动忽略，无需从目录里挪走。）
`make sqlc-verify` 已在 CI 中把关生成代码是否最新，不需要额外机制。

> **`db/query/` 下的 SQL 必须全部是 ASCII。**
> sqlc（v1.30）在计算查询边界时把字节偏移当成了字符偏移：只要文件里出现多字节字符
> （比如一行中文注释），生成的 SQL 常量就会被**静默截断**，运行时报
> `SQL logic error: incomplete input`。截断量正好等于多出来的字节数，
> 因此问题常常在离注释很远的另一条查询上爆炸，极难定位。
> 需要中文说明就写在 `pkg/repo` 的对应方法上。
> `db/query/query_test.go` 会在 CI 里拦下违例。
> 注意这条只约束 `db/query/`；`db/migrations/` 不经过该代码路径，可以正常写中文注释。

### 5.1 双引擎策略：两套 SQL 各自写到最优
<a id="71-双引擎策略两套-sql-各自写到最优"></a>

`db/query/sqlite/` 与 `db/query/postgres/` 本来就是两套独立文件。
**明确放弃「两边 SQL 写法保持一致」这个约束**——各引擎用自己最擅长的表达方式，
只要求 **repo 层暴露的 Go 方法签名与返回语义完全一致**。

这样做的直接收益是：动态 `IN (...)` 与可选筛选不再受 sqlc 跨引擎能力差异的掣肘，
也不必退化成"为每种筛选组合写一条固定查询"的组合爆炸。

#### 变长 IN 列表

| 引擎 | 写法 | 说明 |
|---|---|---|
| SQLite | `WHERE id IN (sqlc.slice(account_ids))` | sqlc 在调用时把它展开成 `IN (?, ?, ?)`，参数是 `[]string` |
| PostgreSQL | `WHERE id = ANY($1::text[])` | 传 `pq.Array`（见 [02 文档 §2.1](02-architecture.md)）。单参数、无长度限制 |

> **实施时的修正**：SQLite 侧原方案是 `json_each(?)`，实际走不通——
> **sqlc v1.30 的 SQLite 解析器不认识 `json_each()` 这个表值函数**：
> 它既不把里面的 `?` 注册为绑定参数，改用 `sqlc.arg()` 时还会把
> `sqlc.arg(account_ids)` 原样留在生成的 SQL 文本里。前者生成的代码少传一个参数，
> 后者直接是非法 SQL——两种都要到运行时才暴露。
> 改用 sqlc 原生的 `sqlc.slice()` 即可，它正是为变长 IN 设计的。
>
> 代价是 SQLite 侧会占用等量的绑定变量（上限 32766，远大于旧版的 999），
> 因此 `pkg/repo` 对单次传入的 ID 数量设了 5000 的上限（`maxInListSize`），
> PostgreSQL 侧虽是单参数也一并设限，以免超大数组拖垮查询计划。

```sql
-- db/query/sqlite/mail_accounts.sql
-- name: ListAccountsByIDs :many
SELECT * FROM mail_accounts
WHERE tenant_id = ? AND deleted_at IS NULL
  AND id IN (sqlc.slice(account_ids));
```
```sql
-- db/query/postgres/mail_accounts.sql
-- name: ListAccountsByIDs :many
SELECT * FROM mail_accounts
WHERE tenant_id = $1 AND deleted_at IS NULL
  AND id = ANY($2::text[]);
```

repo 层吸收差异（两边参数都是 `[]string`，只是字段名不同）：

```go
func (s *Store) ListMailAccountsByIDs(ctx context.Context, tenantID string, ids []string) ([]model.MailAccount, error) {
    if err := checkInList(ids); err != nil {   // 5000 上限
        return nil, err
    }
    if s.driver == "sqlite" {
        rows, err := s.sqlite.ListMailAccountsByIDs(ctx, sqlitedb.ListMailAccountsByIDsParams{
            TenantID: tenantID, AccountIds: ids})
        ...
    }
    rows, err := s.postgres.ListMailAccountsByIDs(ctx, postgresdb.ListMailAccountsByIDsParams{
        TenantID: tenantID, Column2: ids})
    ...
}
```

> `sqlc.slice` / `= ANY` 之后不再需要 [5.2](#52-避免-n1) 提到的「按 500 分块」，
> 但**单条 SQL 传入的 id 数量仍设 5000 上限**（`pkg/repo` 的 `maxInListSize`）。

#### 可选筛选条件

账号列表有 5 个可选条件（分组、关键词、状态、刷新状态、provider）。
两边都用「NULL 即忽略」的写法，但参数绑定方式不同：

```sql
-- sqlite：命名参数 + IS NULL 短路
-- name: ListAccountsPage :many
SELECT * FROM mail_accounts
WHERE tenant_id = :tenant_id
  AND deleted_at IS NULL
  AND (:group_ids   IS NULL OR group_id IN (SELECT value FROM json_each(:group_ids)))
  AND (:q           IS NULL OR email_normalized LIKE :q OR remark LIKE :q)
  AND (:status      IS NULL OR status = :status)
  AND (:refresh     IS NULL OR last_refresh_status = :refresh)
  AND (:provider    IS NULL OR provider = :provider)
ORDER BY sort_order, created_at DESC
LIMIT :limit OFFSET :offset;
```
```sql
-- postgres：sqlc.narg + 数组
-- name: ListAccountsPage :many
SELECT * FROM mail_accounts
WHERE tenant_id = $1
  AND deleted_at IS NULL
  AND (sqlc.narg(group_ids)::text[] IS NULL OR group_id = ANY(sqlc.narg(group_ids)::text[]))
  AND (sqlc.narg(q)::text IS NULL OR email_normalized ILIKE sqlc.narg(q)::text
                                  OR remark ILIKE sqlc.narg(q)::text)
  AND (sqlc.narg(status)::text IS NULL OR status = sqlc.narg(status)::text)
  ...
ORDER BY sort_order, created_at DESC
LIMIT $8 OFFSET $9;
```

**索引友好性的代价**：`(:x IS NULL OR col = :x)` 这种写法会让优化器难以选中索引。
缓解措施：

1. `tenant_id` 与 `deleted_at IS NULL` 是**恒定条件**，放在最前，
   由 `idx_mail_accounts_group`、`idx_mail_accounts_refresh` 等复合索引兜底
2. 对**最高频的两条路径**单独写专用查询，绕开可选条件：
   - `ListAccountsByGroup`（工作台左栏，几乎每次点击都触发）
   - `ListAccountsByRefreshStatus`（Token 管理页默认视图）
3. PostgreSQL 上如果实测仍慢，可为通用查询加 `pg_trgm` 索引支持 `ILIKE`

#### 大小写不敏感的关键词搜索

| 引擎 | 做法 |
|---|---|
| SQLite | 已有 `email_normalized`（入库前小写），`LIKE` 默认对 ASCII 不敏感；关键词在 Go 侧先 `ToLower` |
| PostgreSQL | 用 `ILIKE`，或同样匹配 `email_normalized`。**不要**依赖 `COLLATE NOCASE`（PG 无此语法） |

#### 防止两套 SQL 语义漂移（必做）

两套手写 SQL 最大的风险是**慢慢跑偏**——某天只改了 postgres 那份，sqlite 行为就不同了。
对策是一组**跨引擎对照测试**：

```go
// pkg/repo/parity_test.go
// 同一组种子数据分别灌进 SQLite 内存库与 PostgreSQL 测试库，
// 用同一张 filter 用例表跑 repo 方法，断言两边返回的 ID 序列完全一致。
func TestAccountFilterParity(t *testing.T) {
    cases := []AccountFilter{
        {}, {GroupIDs: []string{"g1"}}, {Q: "OUTLOOK"}, {Status: ptr("banned")},
        {GroupIDs: []string{"g1","g2"}, Q: "a", RefreshStatus: ptr("failed")},
        // ... 覆盖每个可选条件的开/关，以及排序与分页边界
    }
    ...
}
```

PostgreSQL 侧用 CI service container（`.github/workflows/ci.yml` 里加一个 `postgres` service）；
本地无 PG 时该测试自动跳过并打印跳过原因。
**这组测试是双引擎策略成立的前提**，不能省。

### 5.2 避免 N+1
<a id="72-避免-n1"></a>

账号列表要带别名。**不要**在循环里查：

```sql
-- name: ListMailAliasesByAccountIDs :many
SELECT account_id, alias_email FROM mail_account_aliases
WHERE tenant_id = ? AND account_id IN (sqlc.slice(account_ids));
```

这里的 `IN` 同样按 [5.1](#51-双引擎策略两套-sql-各自写到最优) 的方式落地：
SQLite 用 `sqlc.slice()`，PostgreSQL 用 `= ANY($2::text[])`。
repo 层一次取回后在 Go 里组装成 `map[accountID][]string`，
对应 outlookEmail 的 `get_account_aliases_map`
（它因为用普通 `IN (?,?,...)` 而必须按 500 分块；本方案用数组传参后无需分块，
但仍对单次传入的 id 数量设 5000 上限）。

两个方言在这里的写法差异最大，因此 `pkg/repo/parity_test.go` 的
`TestAccountAliasesParity` 专门盯着它。

## 6. 权限模型扩展

权限分两个正交维度，不要混淆：

- **平台角色** `users.platform_role`（`user` / `admin`）——能否跨租户管理整个系统。
  由 `middleware.RequirePlatformAdmin` 校验，不进入下面的 Permission 体系。
- **租户角色** `tenant_members.role`（owner / admin / member）——在某个租户内能做什么。
  即下面这套 Permission。

在个人工作空间模式下，用户在自己的租户里恒为 `owner`，因此拥有全部租户级权限；
member/admin 两档是为未来的团队版预留的。详见 [08 文档 §2.1](08-saas-admin.md)。

`pkg/model/permission.go` 新增：

```go
const (
    PermissionMailGroupRead    Permission = "mail:group:read"
    PermissionMailGroupWrite   Permission = "mail:group:write"
    PermissionAccountRead      Permission = "mail:account:read"
    PermissionAccountWrite     Permission = "mail:account:write"
    PermissionAccountDelete    Permission = "mail:account:delete"
    PermissionAccountSecret    Permission = "mail:account:secret"   // 导出/查看令牌与密码
    PermissionMessageRead      Permission = "mail:message:read"
    PermissionMessageWrite     Permission = "mail:message:write"    // 标已读、删除
    PermissionTokenRefresh     Permission = "mail:token:refresh"
    PermissionAuditRead        Permission = "mail:audit:read"
)
```

角色映射：

| 权限 | owner | admin | member |
|---|:--:|:--:|:--:|
| group:read / account:read / message:read | ✓ | ✓ | ✓ |
| group:write / account:write / message:write | ✓ | ✓ | – |
| account:delete / token:refresh | ✓ | ✓ | – |
| account:secret（导出含令牌） | ✓ | ✓ | – |
| audit:read | ✓ | – | – |

`account:secret` 单独拆出来，是因为「导出账号」等价于**导出全部凭据明文**——
这是本平台风险最高的操作，必须能独立收敛，并且强制写 `audit_logs`。
