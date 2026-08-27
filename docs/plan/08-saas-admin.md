# 08 · SaaS 形态与管理员体系

## 1. 隔离模型：个人工作空间

**决策：保留模板的 `tenants` 层，注册时自动创建一个「个人工作空间」。**

```
用户注册
  → 事务内：创建 user
           创建 tenant（kind='personal', name='<username> 的工作空间'）
           创建 tenant_member（role='owner'）
           创建默认邮箱分组（mail_groups.is_system=1）
           写入 tenant_quotas（取默认套餐）
  → 登录后 session.active_tenant_id 自动置为该个人租户
```

### 1.1 为什么不直接用 `owner_user_id`

- 模板已有的 `tenant.Member` 中间件、`Require(permission)`、`sessions.active_tenant_id`
  全部可以原样复用，改动量最小
- 所有业务表继续用单列 `tenant_id` 隔离，[03 文档](03-data-model.md) 的表设计**一行都不用改**
- 将来要做「团队版 / 子账号 / 代运营」时，只是往同一个租户里加 `tenant_members`，
  不需要数据迁移。对 SaaS 来说这个可选性值钱
- 代价：多一层对普通用户不可见的概念。通过 UI 隐藏（见 §1.3）消化

### 1.2 `tenants` 表的增量

```sql
ALTER TABLE tenants ADD COLUMN kind TEXT NOT NULL DEFAULT 'personal'
    CHECK (kind IN ('personal','team'));
```

规则：

- `kind='personal'` 的租户**不可删除**、**不可添加成员**、**不可改 slug**
  （service 层强制；否则用户删掉自己的个人空间就会变成无处可去的孤儿账号）
- 一个用户有且只有一个 personal 租户；`UNIQUE (created_by) WHERE kind='personal'`
- `kind='team'` 保留给未来，本期不开放创建入口

### 1.3 UI 上的隐藏

- 普通用户登录后直接进 `/mail`，不出现租户选择器
- `/tenant/settings`、`/tenant/members` 两个模板页面对 `kind='personal'` 的租户隐藏入口
  （路由保留，团队版启用时直接复用）
- 「工作空间」这个词只在设置页出现一次（用于展示配额），其余地方不提

## 2. 平台管理员

### 2.1 角色定义

**平台角色与租户角色是两个正交维度**，不要混为一谈：

| 维度 | 存储 | 取值 | 作用 |
|---|---|---|---|
| 租户角色 | `tenant_members.role` | owner / admin / member | 在**某个租户内**能做什么（模板已有） |
| 平台角色 | `users.platform_role` | user / admin | 能否跨租户管理**整个系统** |

```sql
ALTER TABLE users ADD COLUMN platform_role TEXT NOT NULL DEFAULT 'user'
    CHECK (platform_role IN ('user','admin'));
CREATE INDEX idx_users_platform_role ON users(platform_role) WHERE platform_role = 'admin';
```

### 2.2 首个管理员的产生

不用「第一个注册的人自动成为管理员」——这在开放注册的 SaaS 上是明显的抢注漏洞。

```
配置 BOOTSTRAP_ADMIN_USERNAME=admin
启动时：若该邮箱的用户已存在 → 确保 platform_role='admin'
        若不存在            → 不做任何事（等他注册后下次启动生效）
        若配置为空且系统里一个 admin 都没有 → 启动日志打 WARN
```

后续管理员由已有管理员在后台授予。**最后一个管理员不能被降级或删除**（service 层校验）。

### 2.3 跨租户访问的实现方式（关键安全设计）

管理员要能「查看和操作整个系统的邮箱」。有两种实现，必须选对：

| 方案 | 做法 | 评价 |
|---|---|---|
| ❌ 放宽查询 | SQL 写成 `WHERE (tenant_id = ? OR ?::bool)`，管理员传 true 绕过 | **禁止**。一旦查询里存在「可绕过租户」的分支，任何参数传递失误都会变成越权。而且这类分支会渗透到几十条 SQL 里，无法审计 |
| ✅ 显式租户上下文 | 管理员 API 必须显式指定目标 `tenant_id`；service/repo 层的查询**永远**带 `tenant_id = ?`，与普通用户走完全相同的代码路径 | **采用**。SQL 层只有一条隔离规则，永不放宽；差别只在「谁有权决定 tenant_id 的取值」 |

具体形态：

```
普通用户： tenant_id 来自 session.active_tenant_id（tenant.Member 中间件校验成员身份）
管理员：   tenant_id 来自 URL 路径 /api/v1/admin/tenants/:tenantID/mail/...
           由 middleware.RequirePlatformAdmin 校验 platform_role='admin'
           校验通过后把 tenantID 注入 context，下游 service 无感知
```

于是 `AccountService.List(ctx, tenantID, filter)` 这类方法**一份实现两处复用**，
不需要为管理员写第二套 service。这是本设计最重要的收敛。

### 2.4 管理员操作的审计

管理员对他人数据的每一次访问都必须留痕：

- 中间件在 `RequirePlatformAdmin` 通过后，把 `actor_kind='admin'` 写进 context
- 所有写操作照常写 `audit_logs`，`actor_kind='admin'`
- **读操作也要记**：管理员查看某租户的账号列表、查看某封邮件正文、导出账号——
  这三类读操作单独记审计（普通用户的读不记，量太大）
- 管理后台有一个「我的操作记录」页，管理员自己也能看到自己做过什么

### 2.5 不做 impersonation

不实现「管理员一键登录成某用户」。理由：会话被借用后，审计日志里的 `actor_user_id`
会指向被冒充者，事后无法区分是本人操作还是管理员操作。
管理员通过 `/api/v1/admin/*` 直接操作，行为归属永远清晰。

## 3. 用户管理

管理后台能力（对应 `/api/v1/admin/users`，见 [05 文档 §10.1](05-api-design.md)）。
**后台只有这一份清单**：一个租户空间只属于一个用户，单列一份「工作空间」列表的每一行
都能在这里找到对应的用户，两份列表只会让人怀疑「这两个数为什么对不上」——
所以配额与「进入其邮箱」都并进了用户行。

| 能力 | 说明 |
|---|---|
| 用户列表 | 分页 + 搜索，显示：用户名/邮箱/注册时间/状态/平台角色/邮箱账号数/配额用量/最后登录 |
| 禁用 / 启用 | `users.status = 'disabled'`；**禁用时必须同时删除该用户全部 sessions**，否则已登录会话仍然有效 |
| 授予 / 撤销管理员 | 修改 `platform_role`；不能撤销最后一个管理员 |
| 重置密码 | 生成临时密码返回给管理员（一次性显示），并清空该用户全部会话 |
| 调整配额 | 覆盖该租户的配额值，见 §4 |
| 删除用户 | 软删除 user + 级联软删其个人租户下的邮箱账号；二次确认 + 审计 |

`users` 表已有 `status TEXT CHECK (status IN ('active','disabled'))`（模板 `000001_init` 自带），
只需在 `middleware/session.go` 的鉴权路径上加一条：**用户 status 非 active 时会话立即无效**。
这一条目前模板没做，是 P0 必补项。

## 4. 配额体系（不含计费）

### 4.1 数据模型

```sql
CREATE TABLE plans (
    id                  TEXT PRIMARY KEY,
    code                TEXT NOT NULL UNIQUE,      -- free / pro / unlimited
    name                TEXT NOT NULL,
    is_default          INTEGER NOT NULL DEFAULT 0,
    max_accounts        INTEGER NOT NULL DEFAULT 50,    -- -1 表示不限
    max_groups          INTEGER NOT NULL DEFAULT 20,
    daily_mail_fetch    INTEGER NOT NULL DEFAULT 2000,
    created_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 租户的生效配额 = 所属套餐 + 管理员针对该租户的覆盖值（NULL 表示不覆盖）
CREATE TABLE tenant_quotas (
    tenant_id           TEXT PRIMARY KEY REFERENCES tenants(id) ON DELETE CASCADE,
    plan_id             TEXT NOT NULL REFERENCES plans(id),
    max_accounts        INTEGER,
    max_groups          INTEGER,
    daily_mail_fetch    INTEGER,
    note                TEXT NOT NULL DEFAULT '',   -- 管理员备注为什么调额
    updated_by          TEXT REFERENCES users(id),
    updated_at          DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- 按天累加的用量计数（用于 daily_* 类配额）
CREATE TABLE usage_counters (
    tenant_id  TEXT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
    day        TEXT NOT NULL,      -- YYYY-MM-DD，按 quota.DefaultTimezone 计算
    metric     TEXT NOT NULL,      -- mail_fetch（有上限）| token_refresh（只记账）
    count      INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (tenant_id, day, metric)
);
CREATE INDEX idx_usage_counters_day ON usage_counters(day);
```

> `000002_saas` 当初还建了五列给转发 / 对外 API / 本地保留用（`max_api_keys`、
> `max_share_links`、`allow_forwarding`、`allow_retention`、`allow_external_api`）。
> 那些功能移出方案后，这五列已随 `000006_drop_unused`（2026-08-25）从两张表里删除，
> `model.Plan` / `model.Limits` / 后台套餐表单 / 用量页同步清理。上面的 DDL 是清理后的现状。
>
> **跨天点固定按 `quota.DefaultTimezone`（Asia/Shanghai）算**，容器缺 tzdata 时退回 UTC。
> 原计划做成「按租户设置读取」，但承载它的 `tenant_settings` 表随 P5 一起删了；
> 真要做的话得先决定这个设置存在哪。

### 4.2 计算与强制

```go
// pkg/quota/quota.go —— 无状态，只读配置
type Limits struct { MaxAccounts, MaxGroups, DailyMailFetch int }

// 生效值 = COALESCE(tenant_quotas.<col>, plans.<col>)；-1 视为不限
func (s *Service) Effective(ctx, tenantID) (Limits, error)

// 计数类：实时 COUNT（账号数、分组数——量小，不需要缓存）
// 频次类：usage_counters 的 UPSERT + 递增
func (s *Service) CheckAndConsume(ctx, tenantID, metric string, n int) error
```

强制点（**必须在 service 层，不能只在前端**）：

| 配额 | 强制位置 | 超额行为 |
|---|---|---|
| `max_accounts` | `AccountService.Create` / `Import` | 单个创建 → 403 `QUOTA_EXCEEDED`；**导入 → 超额部分计入 `skipped`，已导入的保留**，响应里明确告知「因配额限制跳过 N 个」 |
| `max_groups` | `GroupService.Create` | 403 |
| `daily_mail_fetch` | `MessageService` 每次走远端前 | 403，文案提示明日重置或联系管理员 |

**取件额度对所有来源一视同仁**（网页 / API Key / 管理员共用同一个计数器与同一条上限）。
只对 API 设限是行不通的：会话 Cookie 同样能写进脚本，那等于留下「逆向网页就能绕开」的口子。

**令牌刷新没有额度**（`000013` 删掉了 `daily_token_refresh` 两列）：它是「账号还能不能用」
的前提，卡住它，用户看到的不是「今天少刷一点」，而是一批账号集体登录失败。
真正要防的「批量刷把服务商打到风控」靠的是任务系统的并发数与账号间隔
（`JOB_WORKERS` / `JOB_ACCOUNT_DELAY_MS`）。`usage_counters.token_refresh` 仍然照常累加，
只是没有人拿它去判上限——用量页上那个数字是「是不是有脚本在空转」的唯一线索。

> **导入的超额处理是有意设计成"部分成功"的**。批量平台的用户常常一次粘几千行，
> 整批因为超 3 个而全失败，体验极差且浪费上游调用。

配额降低时（管理员调小 `max_accounts`）**不追溯删除**已有数据，只阻止新增。
后台在该租户上显示「超额 12 个」的告警标记。

### 4.3 前端展示

个人设置页用 Kumo 的 `Meter` 展示各项用量（见 [06 文档](06-frontend.md)），
接近上限时用 `Banner` 提示。

## 5. 注册与登录

### 5.1 注册模式

```
REGISTRATION_MODE = open | closed     （默认 open）
```

- `open`：任何人可注册，自动分配 `is_default=1` 的套餐
- `closed`：只能由管理员在后台创建用户

（原先还规划过 `invite` 邀请码模式，已随 P7 一起删除。要挡注册就用 `closed`。
代码里 `RegistrationInvite` 取值还在，行为等同 `closed`，属于遗留项。）

模板已有的注册限流（0.2 次/秒、burst 10、按 IP）保持不变。

### 5.2 邮箱验证

**不做邮箱验证。** 本平台没有发信能力（SMTP 随转发功能一起从方案中删除，
见 [README「明确不做的事」](README.md)），`users.email` 仅作登录标识与联系方式。

### 5.3 与已有代码的衔接

`AuthService.Register` 需要改成事务化的「用户 + 个人租户 + 成员 + 默认分组 + 配额」五件套。
当前实现只创建 user。这是 P0 的改造项，且必须有一条测试断言五者要么全成功要么全回滚——
中途失败留下一个没有租户的用户，会导致他登录后所有页面 403 且无法自助修复。

## 6. 安全清单（SaaS 特有）

| # | 项 | 措施 |
|---|---|---|
| 1 | 越权读写他人邮箱 | 每个资源端点一条跨租户测试（口径见 `AGENTS.md` §7）；SQL 永不放宽 `tenant_id` |
| 2 | 管理员权限滥用 | 跨租户读写全审计；管理员数量在后台可见；不做 impersonation |
| 3 | 禁用用户仍能操作 | 会话中间件校验 `users.status`；禁用时清空 sessions |
| 4 | 注册滥用 / 薅配额 | IP 限流 + 默认套餐给保守额度 + `closed` 模式可随时切换 |
| 5 | 用户上传的凭据是第三方账号 | 服务条款需明确「用户保证对所托管邮箱有合法授权」；注册页与导入页各放一次提示 |
| 6 | 删除用户后残留凭据 | 删除流程必须物理清除 `refresh_token_enc` 等密文列，而不只是打 `deleted_at` |
| 7 | 平台管理员离职 | 支持撤销管理员 + 全量审计可追溯 |

第 6 条要特别注意：本平台的软删除只对「误删可恢复」有意义，
而**凭据密文不该跟着软删除长期留存**。设计为：软删账号时立即清空三个密文列，
只保留邮箱地址、分组、备注等非敏感元数据供恢复展示（恢复后需要用户重新填凭据）。
