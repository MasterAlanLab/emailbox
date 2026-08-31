# 实施进度

> 本文件由开发循环维护，按时间记录各阶段的完成状态与踩过的坑。
>
> 早期条目里提到的 01/02/07/09/10 号文档（老项目分析、目标架构、分阶段路线图、
> 两次改版记录）已于 2026-08-27 删除——它们是实施前的计划，写完就不再更新。
> 下文保留原文不改写：**日志记的是当时的判断**，把它改成今天的样子等于篡改记录。
> 那几篇的内容在 git 历史里，仍然有效的规则已经并进 `AGENTS.md` 与其余设计文档。
> 状态：`[ ]` 未开始 · `[~]` 进行中 · `[x]` 已完成

## P0 · 地基改造（已完成）

### 1. 基础清理

- [x] 1. 模块改名 `go-react-template` → `emailbox`
- [x] 2. `pkg/crypto`：AES-256-GCM + `HashToken`；configs 加 `ENCRYPTION_KEY` / `APP_ENV`
      （另加 `make gen-key`、`.env.example` 与 `docs/configuration.md` 同步）
- [x] 3. HTTP 装配修正
      - `api.GlobalBodyLimit()`（Skipper 放行 `IsBulkPath`）+ `api.BulkBody()` + 回归测试
      - Gzip `Skipper` 跳过 `handler.IsSSEPath`
      - `handler.SSEWriter`（取消写超时 + keepalive + `Last-Event-ID`）
      - Cookie 已是 `SameSite=Lax`，结论「无需 CSRF token」及其前提已写入 `docs/configuration.md`

### 2. SaaS 基础

- [x] 4. 迁移 `000002_saas`（sqlite + postgres）+ 迁移执行与幂等测试、默认 free 套餐种子
- [x] 5. `AuthService.Register` 事务化（user + 个人租户 + member + 配额，含回滚测试）
      默认邮箱分组待 `000003_mail_core` 到位后加入同一事务（P1）
- [x] 6. `users.status` 校验（`AuthService.Session` 已覆盖，补回归测试 + `UpdateUserStatus`）
- [x] 7. `middleware/platform.go`：`RequirePlatformAdmin` + `actor_kind` 注入 + 非管理员 403 测试
- [x] 8. `BOOTSTRAP_ADMIN_USERNAME` 提权 + 无管理员 WARN（`pkg/service/platform_service.go`）
- [x] 9. `pkg/quota`：`Effective` / `CheckAndConsume`（先加后判、超额回滚）/ `CheckCount` / `Allowance`
- [x] 10. 权限模型扩展（14 个租户级权限 + `PlatformRole` + 权限矩阵测试）

### 3. 双引擎 SQL 基建

- [x] 11. `sqlc.yaml` schema 改为目录（`.down.sql` 被 sqlc 自动忽略）
- [x] 12. `make sqlc-generate` → `make sqlc-verify` 跑通（plans / quotas / usage_counters 查询已生成）
- [x] 13. 双引擎写法落地：SQLite 用 `sqlc.slice()`（原方案的 `json_each` 走不通，见下），PostgreSQL 用 `= ANY($n::text[])`
- [x] 14. `pkg/repo/parity_test.go` + CI postgres service（本地无 PG 时跳过并打印原因）

### 4. Kumo 前端接入

- [x] 15. `bun add @cloudflare/kumo@2.11.0 @phosphor-icons/react`
- [x] 16. `web/src/style.css` 改造（Kumo 两行前置 + 删除 `@theme inline` 与写死颜色）
      过渡期把 `.panel` / `.button-*` / `.field` 改绑到 Kumo 令牌而非直接删除，
      避免迁移中途出现「一半页面没样式」；最后一个使用者迁完后整块删掉
- [x] 17. 删除 `components.json`；lucide-react 已卸载，图标改用 `@phosphor-icons/react`
- [x] 18. 既有页面全部改用 Kumo 组件；`style.css` 的遗留语义类整块删除
      （Home / Login / Register / Dashboard / Layout 之外，Error 与四个设置页也一并迁完）
      - `LinkProvider` + `lib/AppLink.tsx` 桥接 react-router，Kumo `LinkButton` 才不会整页刷新
      - 容器统一用 `LayerCard`（`Surface` 已被 Kumo 标记 deprecated）
- [x] 19. ESLint 拦截原始 Tailwind 色类与 `dark:`
- [x] 20. CI 全程用 bun（版本取自 `web/package.json` 的 `packageManager`），不再装 Node，此项作废

## P1 · 分组与账号（完成）

- [x] 1. 迁移 `000003_mail_core`（groups / accounts / aliases），sqlite + postgres
      （原本还建了 tags / account_tags 两张表，已由 `000007_drop_tags` 删除）
- [x] 2. `db/query/` 两套 SQL：分组 / 账号 / 别名 / 多条件筛选分页（含双引擎变长 IN 与排序变体）
- [x] 3. `pkg/model`：`MailGroup` / `MailAccount` / `AccountFilter` / 各 DTO
- [x] 4. `pkg/repo`：groups.go / accounts.go / aliases.go（批量取别名避免 N+1）
      parity 用例已覆盖变长 IN、批量软删、别名关联
- [x] 5. `pkg/service`
      - [x] `GroupService`：树查询（含递归账号数）、层级校验（≤3）、防环移动、
            子树层级重算、级联删除 + 账号回落默认分组、排序、配额强制
      - [x] 注册事务补上默认邮箱分组（P0 第 5 项的最后一件套）
      - [x] `AccountService`：CRUD（凭据加解密）、别名四种冲突校验、
            分批事务导入（逐行统计 / 超配额计 skipped / on_conflict=update）、六个批量操作
      - [x] `AccountService.Export`：三种范围（选中 / 分组含子树 / 全部）、
            `mailer.FormatLine`（ParseLine 的逆运算）、二次密码验证、限流、强制审计。
            原本卡在「审计表还没有」，P3 的 `000004_platform_admin` 落地后已解除
            - 一条用例钉住三件事：密码错时 403 且一行不出去、导出的文件能重新导入出
              同样多的账号、成功的导出留痕而失败的不留
            - 另跑过一次一次性验证：管理员跨租户导出记的是**管理员本人**且
              `actor_kind=admin`（与 P3 的 `TestAdminCrossTenantAccessIsAudited` 同形态，
              不再单独留一条重复的用例）
- [x] 6. `pkg/handler` + 路由：`/mail/groups` 与 `/mail/accounts` 全部端点（含逐端点越权测试、导入大请求体测试）
- [x] 7. 前端
      - [x] `api/mail.ts` 类型与客户端、`selectionStore`（含单测）
      - [x] `GroupTree`（自建，Kumo 无 Tree）、`AccountList`、`/mail` 页与批量操作条
      - [x] `/mail/import` 导入向导（逐行结果展示）
      - [x] `/settings/workspace` 配额页（Meter + 超额告警 + 套餐功能开关）
      - [x] 自建 `VirtualList`（`@tanstack/react-virtual`），`AccountList` 改 grid 布局以支持虚拟化
      - [x] 账号详情/编辑抽屉（凭据留空即不修改）
      - [x] `GET /api/v1/tenants/:tenantID/quota` 后端端点 + `QuotaService`
      - [x] `ExportDialog`（范围选择 + 二次密码 + 本地下载，界面上明说导出的是明文且会被审计）

## P2 · 邮件协议层（代码完成；真机验收部分完成，缺账号的项见下表）

- [x] 1. `pkg/mailer` 骨架：`Client` 接口、领域类型、`Providers` 表、`ErrKind` 与两个分类函数
- [x] 2. `proxy.go`：解析优先级（整组一起取）、`{mail}` 展开、failover 候选、
      每连接独立的 `Dialer` / `http.Transport`（无全局状态，这是能真正并发的前提）
- [x] 5. `chain.go`：回退链、不可回退错误判定、`OnSuccess` 写回通道
- [x] 上述三块的单测（`chain_test.go` / `errors_test.go` / `proxy_test.go` / `provider_test.go`），
      `pkg/mailer` 覆盖率 90.5%，达到 07 文档「纯函数 ≥85%」的门槛
- [x] 3. `graph/`：token scope 三级降级 + AADSTS 判定、列表/详情/附件、`$batch`（20 条一批、
      失败回退逐条）、429 的 `Retry-After` 退避、代理 failover、`OnTokenRefresh` 轮换回调。
      `httptest` 桩覆盖上述全部路径，覆盖率 86.6%
- [x] 4. `imapx/`：全部完成
      - [x] `utf7.go`：IMAP modified UTF-7 编解码（标准库没有）
      - [x] `folders.go`：服务商候选表 + LIST 结果打分匹配 + 发件箱/草稿箱硬性排除
      - [x] `mime.go`：GBK/GB2312/Big5 头部与正文解码、multipart 递归、附件名净化、
            HTML 摘要；解析炸弹与超大附件都有上限
      - [x] `base64.go`：容忍尾部损坏的 base64（截断的信仍然可读）
      - [x] `client.go` / `auth.go` / `ops.go` / `messages.go`：go-imap/v2（锁 v2.0.0-beta.8）、
            自实现 XOAUTH2（go-sasl 只有 OAUTHBEARER）、IMAP ID、SELECT 两轮回退、
            UID/序列号严格分流、按 EXISTS 算区间的分页、\Deleted + EXPUNGE、代理 failover
      - [x] 测试用 go-imap 自带的 `imapmemserver` 起进程内真服务器，覆盖率 78.2%
- [x] 6. `cmd/mailprobe`：逐通道单独试（不走回退链，否则第一条成功就看不到另外两条是坏的）、
      代理链与失败分类可见、凭据优先从环境变量取以免进 shell 历史
- [x] 7. `MessageService` + 六个端点 + `daily_mail_fetch` 配额（在走远端**之前**扣）
      - `folder=all` 在 service 层拆成收件箱 + 垃圾箱归并，协议层不认识 all
      - 成功通道与轮换后的 refresh_token 写回（三条新的窄 UPDATE，不整行改写）
      - 识别到 banned 立刻置账号状态，后续请求在进协议层之前就被挡下
      - 每个端点一条跨租户 404 测试
- [x] 8. 前端 `/mail` 右两栏（邮件列表 + 详情）、`MessageBody`
      - `MessageList` + `MessageRow` + `FolderTabs` + `MessageBatchBar`：文件夹切换、
        分页「加载更多」（不做滚动自动加载，每页都是一次配额扣减）、批量已读/删除按
        逐封返回结果就地改状态而不重拉
      - `MessageDetail`：头部元信息 + 附件区 + 正文，全屏阅读（Esc 退出）
      - `MessageBody` 拆成组件与 `messageDocument.ts` 两半，净化逻辑成为纯函数并补上
        13 条 XSS 回归（06 文档 §9 的必测项）
      - `MailPage` 从两栏扩成四栏，按 06 文档 §5.1 做响应式：≥1280 四栏并列、
        768~1280 分组树折进筛选栏的 `Select`、<768 单栏层级导航（账号 → 邮件 → 详情）
      - 账号行的点击语义拆开：点邮箱名＝看这个邮箱的信，铅笔按钮＝改配置
      - 顺带把 `AccountFilterBar` / `AccountBatchMenu` 从 `MailPage` 拆出去，
        四个文件都回到 200 行以内（AGENTS.md §5.1.2）

### 验收（07 文档 §P2 的 15 项）

原方案要求逐条用真实账号跑完，现已改为「能写成常驻测试的写测试，其余顺手验」：
真账号拿不齐（Gmail / 163 / QQ 各要一个可用账号加代理），而一次性人工验收
也不会留在 CI 里。`docs/test-accounts.local` 目前只有一个 Outlook 账号
且 Graph 令牌交换失败（当时被笼统标为已过期），靠真账号跑通的是下表几项。

| # | 项 | 结果 |
|---|---|---|
| 3 | Graph 不可用时回退 | ✅ `graph` 失败后 `imap_new` / `imap_old` 均拉到 4 封 |
| 5 | Graph 令牌交换失败 | ✅ 判为 `auth_failed`，但这只证明 Graph 通道拒绝了本次交换，不证明 refresh_token 整体失效。原设计要求就此停手，**实测推翻**：两条 IMAP 仍能拉到邮件。已改用 `RetriableFrom` 按通道和错误来源判断回退（见下） |
| 10 | 附件 | ✅ `-detail` 路径跑通（该账号的信无附件，只验证了链路） |
| 12 | `{mail}` 模板代理 | ✅ 转为常驻测试 `TestProxyIdentityIsPerConnection` |
| 13 | 20 账号并发 | ✅ 转为常驻测试 `TestConcurrentListsAreActuallyParallel` |
| 14 | 分页无重叠遗漏 | ✅ 已有 `TestListPagination` 覆盖 |
| 15 | 恶意 HTML | ✅ `messageDocument.test.ts` 13 条用例 |
| 1 2 4 6 7 8 9 11 | 其余 | ⬜ 缺可用账号（有效 Outlook OAuth / Gmail / 163 / QQ）与代理，不作为交付门槛 |

第 13 项做了变异验证：在 `imapx.Client.dial` 里临时加一把全局锁，测试如期失败并给出
「协议层存在全局串行点」，随后还原。一条永远不会红的并发断言等于没写。

> 注：`importer.go` 虽在本包，但它是 P1 的批量导入解析，已完成。

## P3 · 管理后台（完成）

- [x] 1. `AdminService`：用户列表/详情/改状态与角色/重置密码/删除
      - 禁用与重置密码都清空该用户全部 sessions（只改 status 而留着会话，「禁用」就是个摆设）
      - 最后一个管理员不可降级、不可删除；管理员也不能停用或删除自己
      - 删除用户走一个事务：清邮箱（同一条 UPDATE 里清空三个密文列）→ 删个人空间 → 软删用户 → 清会话
- [x] 2. 套餐 CRUD（`code` 不可改、默认套餐唯一且不可删、在用套餐不可删）
      + 租户配额覆盖（`note` 是硬性要求，缺了直接 400）
- [x] 3. 跨租户邮箱路由：`/admin/tenants/:tenantID/mail/**` 与用户侧**同一份路由表**
      （`api.mountMailRoutes` 挂两次），`middleware.Require` 对平台管理员放行，
      SQL 仍照常带 `WHERE tenant_id = ?`；跨租户 Banner 由前端按路由渲染
      （曾有 `X-Admin-Context` 响应头，2026-08-26 删除，见下）
- [x] 4. 审计：`AuditWrite` 记全部写操作（actor_kind 区分 user / admin），
      `AuditAdminRead` 只记管理员的读（账号列表、邮件正文、附件）
- [x] 5. `/admin/stats` 平台概览（八个计数一条 SQL 取齐）
- [x] 6. 前端：`/admin` 总览、`/admin/users`、`/admin/tenants`、`/admin/plans`、`/admin/audit`、
      `/admin/tenants/:id/mail`（复用整棵 `/mail` 组件树 + 常驻管理员 Banner）
      - `AdminRoute` 守卫；`mailBase()` 的 scope 切换及其 8 条单测
- [x] 迁移 `000004_platform_admin`：audit_logs + `users.last_login_at`
      （03 文档原本把 audit_logs 和 P4 的 jobs 打包在 `000004_mail_ops` 里，
      审计是 P3 的硬依赖而 jobs 不是，已拆开并同步修正文档的迁移表）

### 验收（07 文档 §P3 的五条，全部有测试）

| 项 | 测试 |
|---|---|
| 非管理员访问每个 `/admin/*` 端点均 403 | `TestNonAdminIsRejectedOnEveryAdminEndpoint`（22 个端点逐条） |
| 管理员在他人租户的操作 actor 是本人、`actor_kind=admin` | `TestAdminCrossTenantAccessIsAudited` |
| 禁用用户后会话立即失效、启用后可重新登录 | `TestDisablingUserKillsSessionImmediately` |
| 调低配额只挡新增、不追溯，后台显示超额标记 | `TestLoweringQuotaBlocksNewAccountsButKeepsExisting` |
| `mailBase()` 的 scope 切换不会误打到自己的租户 | `mailScope.test.ts` |

另外真机起了一次服务走通全流程：普通用户 403 → `BOOTSTRAP_ADMIN_USERNAME` 提权 →
管理员跨租户读到对方邮箱（响应头带 `X-Admin-Context`）→ 审计里两条记录的
`actor_kind` 分别是 user 与 admin → 调低配额后对方新增被 `code=1001` 拒绝。

## P4 · 任务系统与 Token 刷新（完成）

- [x] 1. 迁移 `000005_mail_ops`（jobs / job_items / job_events / mail_refresh_logs），
      sqlite + postgres。编号顺延到 5：`audit_logs` 是 P3 的硬依赖，已随
      `000004_platform_admin` 先行落地
- [x] 2. `pkg/job`：`Manager`（提交 / 查询 / 停止 / 心跳）、worker pool、事件写入
      - `ReapStale` 在启动时把心跳超时且未终结的任务标为 `interrupted`（main.go 调一次）
      - 停止是协作式的：正在处理的账号跑完，还没轮到的落 `skipped` 而不是留在 pending
- [x] 3. SSE：`handler.SSEWriter` + `broker` 广播 + `job_events` 回放
      （`Last-Event-ID` 头与 `?last_event_id=` 都认），一次最多补 200 条
- [x] 4. `RefreshService`：单个刷新、批量提交、`refresh_token` 轮换写回 +
      `last_refresh_*`；`daily_token_refresh` 在**提交时**按账号数一次性预扣
      （跑到一半再拒绝等于让用户为半批结果付全额）
- [x] 5. `/refresh/stats` 按 `error_kind` 聚合 + `/refresh/logs`
- [x] 6. 前端 `/mail/tokens`（统计块 + `Meter` 进度 + 明细）与 `jobStore`（SSE 订阅与续看）

### 验收

| 项 | 测试 |
|---|---|
| 批量刷新可停止、剩余项落 skipped | `TestStoppedJobSkipsRemainingAccounts` |
| 强杀进程后僵尸任务标为 interrupted | `TestStaleJobIsMarkedInterrupted` |
| 失败按 error_kind 各归各类 | `TestRefreshStatsGroupsByErrorKind` |
| 并发写 jobs 计数无竞争 | `TestJobCountersDoNotLoseIncrements`（CI 跑 `-race`） |
| 任务只能被所属租户查看 | `TestJobsAreIsolatedAcrossTenants` |
| 断线重连不重发已确认事件 | `TestJobStreamReplaysFromLastEventID` |

> 大批量（5000 账号）压测已从验收条件里去掉：几十个账号足以覆盖「进度 / 停止 /
> 恢复」这三条行为，真实规模下的速率由配额天然限速。

## P5–P7 · 已删除（2026-08-21）

转发与调度、对外能力（API Key / 对外 API / 分享链接 / 本地邮件保留）、增强（临时邮箱、
邀请码、备份、多实例、计费等）**已从方案中删除**，不是延后。清单与理由见
07 文档 §5。**P4 完成即等于全部范围完成。**

### 遗留物清理（2026-08-25，已完成）

当初为这些功能预埋的东西已全部删除。触发清理的不是洁癖：`plans` 的三个 `allow_*`
开关在后台套餐表单里是**可点的**，用量页还把它们当作套餐权益展示给用户——
点了之后什么都不会发生。一个开关式的谎言比没有这个开关更糟，所以连同背后的列一起拔掉。

| 遗留物 | 原位置 | 处理 |
|---|---|---|
| `forward_enabled` / `forward_cursor` / `forward_last_checked_at` 三列 + `idx_mail_accounts_forward` | `000003_mail_core` | `000006_drop_unused` 删列；repo/service/handler 与前端类型同步清空 |
| 账号筛选里的 `forward_enabled` 条件 | `db/query/{sqlite,postgres}/mail_accounts.sql`（每套 9 处） | 删除，`parity_test` 的两条对应用例一并移除 |
| `POST /accounts/batch/forwarding` 路由与 `BatchForwarding` handler/service | `api/routes.go` | 删除 |
| `max_api_keys` / `max_share_links` / `allow_forwarding` / `allow_retention` / `allow_external_api` 五列 | `000002_saas`（`plans` 与 `tenant_quotas` 各一份） | `000006_drop_unused` 删列；`model.Plan` / `model.Limits` / 后台套餐表单 / 用量页同步 |
| `MetricExternalAPICall` 常量 | `pkg/model/quota.go` | 删除 |
| `mail:forward:manage` / `mail:apikey:manage` / `mail:share:manage` / `mail:settings:manage` 四个权限常量 | `pkg/model/permission.go` | 删除，权限矩阵测试同步 |
| `MessageListResult.Source`（恒为 `remote`） | `pkg/service/message_service.go` | 删除 |
| `RegistrationInvite`（行为等同 `closed`） | `configs/config.go` | 删除，`REGISTRATION_MODE` 现在只接受 `open` / `closed` |

**连带清掉的**：`quota.RequireFeature` / `quota.ErrFeatureNotAllowed` / 业务码 `1002`
（当前套餐未开放该能力）。五个 `allow_*` 列没了之后，Limits 里再没有开关类配额，
这条错误链路无人能产生——一个永远不会被返回的响应码同样是对外撒谎。
`repo.nullBoolInt64` / `nullBoolInt32` 也随之无人调用，由 lint 的 `unused` 检出后删除。

**做法**：SQLite 不允许 DROP 一个仍被索引引用的列，`000006` 必须先 `DROP INDEX
idx_mail_accounts_forward` 再删列。SQLite 的 DROP COLUMN 是整表重建，因此专门验证过
「升级已有库不丢行、不串列、其余索引仍在」，而不只是跑一遍全新库的迁移。
sqlc v1.30 能正确解析 `ALTER TABLE ... DROP COLUMN`，生成结果无需手工干预。

### 删除 `/health` 与 `X-Admin-Context`（2026-08-26）

两个都属于「写了但没人用，且承诺了一件它做不到的事」。

| 删除物 | 原位置 | 理由 |
|---|---|---|
| `GET /api/v1/health` | `api/routes.go` | 返回静态 JSON，不碰数据库也不看静态文件。`docker-compose.yml` 正拿它做 healthcheck，于是数据库挂掉、前端资源缺失时容器仍报健康——编排器既不重启也不摘流量。连带删掉 compose 里的 healthcheck 块（只删路由的话它会打到 SPA catch-all 拿 404，容器永久 unhealthy） |
| `X-Admin-Context` 响应头 | `pkg/middleware/platform.go` | 设计初衷是「两套邮箱路由完全同构，响应体里分不出数据属于谁，用响应头补一句」。但前端从未读过它，Banner 一直是 `AdminTenantMailPage` 按路由渲染的；而管理员身份本就在会话的 `platform_role` 里（`AdminRoute` 用的就是它），不需要逐个响应重复声明。`api/admin_test.go` 里那三行断言一并删除，该用例本身（审计 actor 与 actor_kind）不受影响 |

`middleware.TenantContext` **保留**：它还兼着「校验 URL 上的租户确实存在」，
少了这步管理员把 tenantID 打错会拿到空列表而不是 404，两种情况的排障方向完全相反。

### Graph 的 `auth_failed` 改为继续回退（2026-08-27）

原设计（04 文档 §3、07 文档 §5 验收表第 5 项）要求 `auth_failed` 一律停止回退，
理由是「三条通道用的是同一个 refresh_token，一条认证失败则三条都会失败」。
**token 确实是同一个，但请求的资源与 scope 不同**——Graph 要
`https://graph.microsoft.com/...`，项目的新版 IMAP 兼容通道请求
`https://outlook.office.com/IMAP.AccessAsUser.All offline_access`，旧版兼容通道按历史实现使用
`login.live.com` 且省略 scope。微软完全可能拒掉一套
而照常签发另一套：管理员回收了 Graph 权限、应用注册变更、账号本就只被授予过 IMAP scope，
都会打到这个组合上。按旧逻辑这类账号在 Graph 一步就被判死，尽管 IMAP 拉信完全正常。

mailprobe 逐通道实测确认了这一点：Graph 报 `auth_failed`，两条 IMAP 都拉到了邮件。

改法是加 `mailer.RetriableFrom(channel, err)`。2026-08-27 首次放宽只针对 Graph；
2026-08-30 又把 IMAP OAuth 令牌端点与 IMAP 协议登录分开：前者有 HTTP `StatusCode`，
认证/权限失败可以继续换通道；后者没有 HTTP 状态码，`auth_failed` 仍然立即停手。
`banned` 与整体取消始终立即停手，`Retriable` 保留单错误的原语义，回退链用
`RetriableFrom` 结合通道与错误来源判断。

顺带修掉：`MessageService.Detail` 在协议层返回 nil slice 时补齐空数组。
Go 的 nil slice 序列化成 `attachments: null`，而前端类型声明的是非空数组，
`AttachmentList` 里的 `null.filter(...)` 会被 react-router 的 `errorElement` 接住——
用户丢的是整个页面而不只是附件区。绝大多数邮件都没有附件，这是打开详情的**主**路径。

### 分组管理页（2026-08-27）

分组这个功能此前**只有一半**：后端的树、配额、三级校验、删除回落全都齐备，
`db/query` 与 repo 也都在，但前端五个写接口（`createGroup` / `updateGroup` /
`moveGroup` / `reorderGroups` / `deleteGroup`）**一个调用方都没有**。
结果是每个用户的分组树里永远只有注册时自动建出的那一个系统分组，
导入页的「导入到分组」也就永远只有一个选项——看起来像个摆设，其实是入口没建。
06 文档 §5.2 里列的 `GroupFormDialog.tsx` 从一开始就没落地。

新增 `/mail/groups`，全局导航里「标签」的位置换成「分组」。行内 `⋯` 菜单提供
编辑、上移下移、删除。两条禁用规则都对应后端必定拒绝的操作，
在前端先挡掉——让用户点了再拿一句报错，等于把规则藏起来让他去撞：

| 禁用 | 后端对应 |
|---|---|
| 系统分组的「删除」 | `GroupService.Delete` 拦 `is_system`。它还是所有账号的回落目标，没了之后删任何分组都会失败 |
| 达到 `max_groups` 时的「新建分组」 | 配额返回业务码 1001 |

删除确认框专门写明「里面的账号会回到默认分组、不会被删除」。
`Delete` 是先 `MoveAccountsToGroup` 再删的，不说清楚的话用户看到「删除分组」
只会想到里面那几百个账号没了，于是要么不敢删、要么删完花半小时确认账号还在。

**分组配色不能用 `bg-kumo-*`**：Kumo 的 purple 只有文字色令牌，`bg-kumo-purple`
**不会被生成**——实测构建产物里没有这条规则，紫色分组的圆点会是透明的。
六个色一起在 `style.css` 里定义成 `--color-ebx-group-*`（同 `--color-ebx-dot-*` 的做法），
免得五个走 Kumo、一个走别处。这是 `StatusDot` 注释里提过的那类静默失败，
只在生产构建里露出来（dev 有 JIT 兜底）。

**`/mail` 左栏的 1280 断点保持不变**：它是 06 文档 §5.1 量过之后定的，
768~1280 之间分组切换折进筛选栏的 `Select`，并没有消失。降到 1024 的话，
1024 - 224（导航）- 300（左栏）只剩 500px 给账号列表和邮件列表分，
正是 09 文档记录过的那个「三栏挤在一起每栏都不够读」。

### 注册不再需要邮箱，登录改认用户名（2026-08-27）

注册只要用户名和密码；邮箱降级为「登录后可填、也可以一直不填」的资料字段。
登录身份随之从邮箱换成用户名——**一个可以不填的字段当不了凭据**，
否则「没设邮箱的人怎么登录」就没有答案。

`users.email` 的 NOT NULL 保留（空串表示未设置），但表级 UNIQUE 必须去掉：
它只允许**一个**人是空串，第二个不填邮箱的人会直接撞唯一索引。
换成部分唯一索引 `WHERE email <> ''`，只约束真正填了邮箱的行。

**连带改掉的三处**，都是「原本靠邮箱认人」而邮箱不再保证存在：

| 位置 | 改法 | 不改会怎样 |
|---|---|---|
| `BOOTSTRAP_ADMIN_EMAIL` → `BOOTSTRAP_ADMIN_USERNAME` | 按用户名提权 | 一个所有人都没填邮箱的部署将永远产生不出管理员，后台永远进不去 |
| `POST /tenants/:id/members` 的 `email` → `username` | 按用户名加成员 | 没填邮箱的同事根本没法被加进租户 |
| `audit_logs.actor_email` → `actor_name`（存用户名） | 000009 改名 | 该字段的用途是「用户被删、外键置空后仍能追溯是谁」，用邮箱做兜底会在最需要追溯时正好是空的 |

`UpdateProfileRequest.Email` 也从 `string` 改成 `*string`：用 string 的话空串会被
当成「没传」，于是邮箱设错了想删掉这条路根本走不通（nil=保持，""=清空，同 AvatarURL）。

**迁移执行器新增 `-- migrate:no-transaction`**（`migrate.go` 的 `NoTxDirective`）。
SQLite 去不掉写在列上的 UNIQUE，只能整表重建；重建要 DROP 掉 users，而
tenants / tenant_members / sessions / audit_logs 四张表都有外键指着它。
官方做法是先 `PRAGMA foreign_keys=OFF`，但**该 PRAGMA 在事务内是空操作**，
而执行器把每个文件都包在一个事务里。试过的两条弯路都不通，记下来免得再试：

- `PRAGMA defer_foreign_keys=ON`：只把检查推迟到 COMMIT，而 DROP 记下的
  违规计数**不会**因为把表建回来而清零，COMMIT 时照样报 FOREIGN KEY constraint failed
- `PRAGMA legacy_alter_table=ON`（想让 RENAME 不改写子表引用）：在事务内同样被忽略

用了该指令的文件自己写 BEGIN/COMMIT，并且**必须可重复执行**——版本号是在文件
跑完之后才记的，两者之间崩溃会重跑一遍。`TestNoTxMigrationsAreRepeatable` 钉住这条：
它把版本记录抹掉再跑一次，断言数据不变。

**顺带补上了之前欠的那个测试**：`TestUpOnPopulatedDatabase` 先建到 000007、灌进
用户/租户/成员/会话/审计各一行，再跑完剩下的迁移，断言行不丢、列不串、外键仍成立。
此前迁移只在**全新库**上测过——而整表重建恰恰是空库上永远不会出问题、
有数据时才翻车的那类操作。

## 过程中发现的坑

- **令牌刷新与收信曾使用不同通道策略**：刷新只检查 Graph，收信却可走 IMAP OAuth，
  导致「刷新失败但照常收信」。刷新应复用同一通道链，IMAP 刷新只请求 OAuth 端点，
  不用取邮件证明成功。`invalid_grant` 是大类，不代表已确认过期；标准 OAuth `error`
  只给出粗粒度类别，微软结构化 AADSTS 码只作为 best-effort 诊断，用来进一步区分
  过期、撤销、权限与客户端配置。Graph 与 IMAP OAuth 端点的认证/权限失败
  都要允许跨通道回退；IMAP LOGIN / XOAUTH2 失败、封禁和整体取消仍停止。
  另外旧版 IMAP 兼容请求不带 scope；v2 平台轮换新值不会因本次使用就吊销旧值，
  但这个保证不直接扩大到 `login.live.com` 历史端点。

- **HTTPS 代理的 407 出现在 `Do` 的 error，不一定有 HTTP Response**：CONNECT 阶段的
  `Proxy Authentication Required` 与 SOCKS5 认证失败要先归 `proxy_failed` 并停止代理候选；
  当成普通 `network` 会触发直连兜底，造成用户配置了代理却从服务器公网出口请求上游。

- **`db/query/` 下的 SQL 必须全部 ASCII**：sqlc v1.30 遇到多字节字符会静默截断生成的 SQL
  常量，运行时报 `SQL logic error: incomplete input`，且爆炸点常在另一条查询上。
  已加 `db/query/query_test.go` 拦截，并记入 [03-data-model.md §5](03-data-model.md)。
- 模板既有的「删除租户」用例跑在注册自动创建的租户上，个人空间禁止删除后需改用团队空间。
- **路由级 `BodyLimit` 不会覆盖全局的那个**（05 文档原方案的设想不成立）：Echo v5 两层都生效、
  更严的说了算。必须「全局 Skipper 放行 + 路由级重新限制」成对使用，已修正 05 文档并加测试。
- `TestPostgresMigrations` 原本逐表 DROP，每加一张表都要同步清单；已改为重建 schema。
- **sqlc v1.30 的 SQLite 解析器不认识 `json_each()`**（03 文档 §5.1 的原方案）：
  它不把里面的 `?` 注册为绑定参数，改用 `sqlc.arg()` 时还会把 `sqlc.arg(...)` 原样留在
  生成的 SQL 文本里——两种都要到运行时才炸。改用 sqlc 原生的 `sqlc.slice()`，已修正 03 文档。
- **`strings.Trim(line, "----")` 会吃掉密码末尾合法的 `-`**：Trim 的第二个参数是字符集合而非后缀。
  这是静默篡改凭据的 bug，由 staticcheck SA1024 发现，已改为「先切分再丢弃尾部空字段」并加回归测试。
- `github.com/lib/pq` 进入 go.mod 但**不作为驱动使用**，只为 sqlc 生成的 `pq.Array` 数组编码，
  已记入 02 文档 §2.1。
- **sqlc 完全无法参数化 `ORDER BY`**：`sqlc.arg()` 放进去会被原样留在生成的 SQL 里，
  裸 `?` 则被静默丢弃——两者都要到运行时才炸。改为每个「排序字段 × 方向」各一条查询
  （8 条），由 service 按白名单分派；两个方言的 `.sql` 由同一个脚本生成以防漂移。
- **`LIKE ... ESCAPE` 与 `sqlc.narg()` 不能共存**（sqlc 的 SQLite 解析器报
  `no viable alternative at input 'LIKE'`）。改用字面子串匹配：SQLite 用 `instr()`、
  PostgreSQL 用 `strpos()`。对搜索框来说这本来就更符合直觉（用户搜 `%` 就是搜 `%`），
  且两个引擎语义完全一致，无需转义。
- **React 19 的 `react-hooks/set-state-in-effect` 规则值得听**：它拦下了我在 effect 里
  同步 setState 的写法。改成「await 之后再 setState」并顺带加了 `ignore` 竞态守卫——
  用户快速切换分组时，先发的慢请求后到会把新筛选的结果覆盖掉，这是个真实的 bug。
- **代理地址是加密存的，打码前必须先解密**。我第一版直接对密文调 `MaskProxyURL`，
  界面上会显示 `enc:v1:...`。分组代理原本还完全没加密，一并补上——分组代理和账号代理
  一样可能带认证口令。解密失败时回显「(无法解密)」而不是密文或残缺口令。
- **协议层的写回必须是窄 UPDATE**：`auth_channel` / `refresh_token` / `last_refresh_*`
  在每次拉信时都会写，用现成的 `UpdateMailAccount`（整行改写）会把用户此刻正在编辑的
  分组、备注、代理一起覆盖掉。已加三条各只改自己那几列的 SQL。
- **`mailer.Client.Attachment` 原来带不回 `id_mode`**：04 文档 §5.4 明确要求「详情/附件请求
  必须带回同样的 id_mode」，但接口签名里只有 `msgID`。IMAP 上按 UID 去取序列号标识的邮件
  （或反过来）会取到另一封信的附件。已给接口加上 `idMode` 参数——Graph 用不到但也无害，
  总好过在 imapx 里硬编码一个「反正我们只用 UID」的假设。
- **go-sasl 没有 XOAUTH2**（只有 OAUTHBEARER，两者不通用），已自实现。
  关键点是失败时那一轮：XOAUTH2 出错时服务器不直接回 NO，而是先发一个带 JSON 错误详情的挑战，
  客户端必须回一个空串才结束。不回的话连接一直挂着——批量刷新时表现为「卡死」而不是「失败」。
- **04 文档的 IMAP 文件夹表把「已发送」写成了「已删除」**：QQ/163/126 与 2925 的
  `FolderDeleted` 候选原本是 `&XfJT0ZABkK5O9g-` / `&XfJT0ZAB-`，解码出来是
  **已发送邮件 / 已发送**。照抄的话用户点「已删除」会 SELECT 到发件箱——
  看到自己发出去的信，在那里删除就是真的删发件箱。正确值 `&XfJSIJZkkK5O9g-` / `&XfJSIJZk-`，
  文档与代码都已修正。这类错误肉眼 review 看不出来，已加
  `TestEncodedFolderNamesDecodeToWhatTheyClaim` 逐条解码校验整张表，
  另加 `neverMatch` 名单让打分匹配永远不会选中发件箱/草稿箱。
- **代理配置写错时不能滑到直连**。`ProxyCandidates` 末尾留直连是有意的兜底，
  但那只针对「配置正确但连不上」。协议不支持、URL 非法这类**构造阶段**就失败的情况，
  顺着候选链走下去会变成「用户以为流量走代理、实际从服务器公网 IP 直连」——
  这恰恰最容易触发服务商风控，也最难被发现。已在 `graph.withSession` 里区分两者：
  构造失败当场报 `proxy_failed`，连接失败才 failover。写 `TestProxyFailureIsAttributedAndMasked`
  时被这个测试逼出来的，原实现确实会静默直连。
- **`mailer.Error.cause` 不导出**，子包自己 `&mailer.Error{...}` 的话 `errors.Is` 就断在那里，
  底层的 `context.Canceled` / `net.Error` 全都找不回来。已加导出的 `mailer.NewError`。
- **Kumo 的 `LinkButton` 默认渲染原生 `<a>`**：直接用会让站内跳转变成整页刷新——
  丢掉 SPA 状态还闪白屏，而且本地点着「能跳转」，很容易漏掉。必须在根部装
  `LinkProvider` 注入 react-router 桥接组件（`web/src/lib/AppLink.tsx`）。
  桥接组件要同时读 `href` 与 `to`（`LinkButton` 两个都传），外链和锚点仍走原生 `<a>`。
- **`Surface` 已被 Kumo 标记 deprecated**，是 `LayerCard` 的兼容壳。新代码直接用 `LayerCard`；
  它的 `render` prop 可以把容器渲染成 `<form>`，正好替掉原来的 `<form className="panel">`。
- 已加 `TestGeneratedSQLHasNoLeftoverDirectives`：扫描生成产物里是否残留 `sqlc.arg` /
  `sqlc.slice` 等指令。上面两个坑都属于「sqlc 静默留下非法 SQL」，这类问题必须在 CI 拦住。
- **DOMPurify 的 `ALLOWED_URI_REGEXP` 管不到 `img/video/audio/source/track` 的 `src`**：
  它内部对这几个标签（`DATA_URI_TAGS`）有条捷径——只要以 `data:` 开头就直接放行。
  于是原来写着「data: 只允许图片」的配置，实际上对 `<img src="data:text/html;...">`
  完全不生效。这类载荷在 `<img>` 里本来也渲染不出东西，真正的问题是
  「配置写了却不生效」：迟早有人在这份配置上再放宽一点，然后踩中。
  已在 `afterSanitizeAttributes` 里补一刀，写用例的时候才发现的。
- **DOMPurify 删掉 `<form>` 时默认保留子节点**（`KEEP_CONTENT`），于是钓鱼邮件里那个
  「请重新登录」的密码输入框会原样渲染在正文里。sandbox 让它提交不出去，但一个长得像
  登录框的东西出现在邮件里，本身就是在教用户往那儿填密码。已把表单控件加进 `FORBID_TAGS`。
- **`react-hooks/set-state-in-effect` 第二次拦住同一类写法**：这次是邮件列表切文件夹时
  在 effect 里连着 setState 五次。改成「把整批数据收拢成一个带 `key` 的 state 对象、
  在渲染期同步重置」（React 官方的 adjusting-state 模式）。这不只是让 lint 闭嘴：
  用 effect 重置会晚一帧，那一帧里旧文件夹的邮件挂在新文件夹的标签下，
  此时点批量删除删的是看不见的信——和之前账号列表那个 bug 是同一个形状。
- **并发断言要做变异验证**。`TestConcurrentListsAreActuallyParallel` 写完先在
  `imapx.Client.dial` 里临时加了把全局锁，确认它确实会红，再还原。顺带发现失败路径
  要跑 190 秒（20 次拨号各等满自己的超时），已改成第一个超时的人关掉 `giveUp` 通道
  让其余立刻返回，失败用例降到 5 秒。
- **`WithTx` 原来不可重入，而这在 SQLite 上是会挂死整个进程的**。有些 repo 方法自带事务
  （`DeleteTenant` 要把软删和清理会话绑在一起），P3 的「删用户」把它组合进更大的事务后就嵌套了。
  SQLite 的生产配置只有一个连接（`pkg/database`），内层 `BeginTx` 会去等一把外层自己攥着、
  永远不会释放的锁——不是报错，是静默挂死；PostgreSQL 那边则是拿另一条连接，两个事务
  互相看不见对方的未提交数据，更隐蔽。已让 `WithTx` 在已处于事务中时直接复用当前句柄，
  并加 `TestWithTxIsReentrant`（带 10 秒超时，挂死时会失败而不是把 CI 卡住）。
  测试环境没有 `MaxOpenConns(1)`，所以它表现为 SQLITE_BUSY 被当场发现——这次运气不错。
- **`sqlc.arg(day)` 撞上 `usage_counters.day` 列名会报「column reference is ambiguous」**，
  且报错指向的是列不是参数，很容易往错的方向查。改名 `for_day` 不够，还要给子查询里的表
  起别名并逐列限定（`uc.day`）才能过。
- **管理员的跨租户放行只能有一处**。`middleware.Require` 里为平台管理员开了个口子，
  代价是必须保证 `platform_user` 只由 `RequirePlatformAdmin` 设置——它是这个口子的唯一来源。
  换成「给管理员合成一个 tenant_member」的写法会更糟：那个假成员会流进审计和业务判断，
  分不清是真成员还是管理员。
- **邮件正文的裸 NUL 分隔符会让整个源文件变成二进制**：`refKey` 用 U+0000 拼
  `folder` 与 `id` 是对的（这两者都不可能含 NUL，不会撞键），但写成裸控制字符之后
  git 与 grep 都把 `MessageList.tsx` 当二进制，diff 直接看不了。改成 `\u0000` 转义写法。
- **导出的分支必须与 `detectFormat` 的判据严格对偶**。`FormatLine` 写完先跑往返用例
  （FormatLine → ParseLine → 比对），因为「导出的文件能重新导入」是这个功能唯一的
  硬性要求，而写错了看不出来——导出的文本肉眼读着完全正常，只有再导入一次才会
  发现 client_id 和 refresh_token 换了位置。另外凭据里含 `----` 时宁可报错也不导出：
  那一行重新导入会被切成完全不同的字段，属于静默的数据损坏。
- **账号列表的分页 SQL 只认一个 `group_id`**（双引擎各写一份，变长 IN 只用在批量取
  别名上）。按分组导出要展开到子树，于是只能逐个分组取回再按 ID 去重合并。
  一开始按 `GroupIDs: [...]` 传进去是静默少给：SQL 只取了列表里的第一个。
- **前端拿导出结果不能用 `responseType: "blob"`**：出错时后端回的仍是 JSON，
  按 blob 收会让 axios 拦截器读不到 `message`，界面上只剩「请求失败 (403)」——
  用户分不清是密码打错了还是被限流了。改用 `"text"`，axios 的默认 transform
  仍会把 JSON 错误体解析成对象，两种情形都能拿到文案。
- **导出限流要按用户而不是 IP**。同一个办公室出口 IP 后面可能有几十个用户，按 IP 限
  会互相误伤；而攻击者拿到会话之后换 IP 是零成本的，按 IP 也拦不住。
- **测试口径已收敛**（07 文档 §1）：只测核心行为与安全边界，
  不做防御性堆砌，新增用例优先并进已有文件而不是为一个函数新开 `_test.go`。
  据此删掉了 P4 的四条镜像式用例（心跳正常不被回收、已结束任务不能停止、
  刷新日志逐条落库、无令牌账号被排除）——它们要么是另一条用例的反面，
  要么断言的是永远不会红的东西。
- **导出为空时不能照样下载**。没有凭据的账号会被后端跳过，整批都没凭据时导出结果是空的，
  照样触发下载会让用户以为导出成功了——直到过几天拿这个文件去恢复才发现里面什么都没有。
  条数从内容里数而不是读 `X-Export-Count`：`main.go` 的 CORS 没配
  `Access-Control-Expose-Headers`，前后端分域部署时那个头在浏览器里根本读不到。
  （`X-Admin-Context` 原本有同样的问题，该头已于 2026-08-26 删除。）
- **Kumo 的暗色是显式开关，不跟随系统**：它声明的是 `:root{color-scheme:light}` 与
  `[data-mode="dark"]{color-scheme:dark}`，而所有颜色令牌靠 `light-dark()` 解析。
  没人给根元素挂 `data-mode` 的话，写再多语义令牌也永远是亮色——06 文档
  「亮/暗模式下均正常」这条验收此前根本无法抵达。已加 `web/src/lib/theme.ts`
  （首屏前挂 `data-mode`、跟随系统、可手动切换并记住选择）与顶栏的切换按钮。
- **审计页的「操作者」列此前是一串裸 UUID**：写入侧只有管理员路径带 `actor_email`
  （普通用户的 context 里没有完整用户对象）。一屏 UUID 等于没有审计，谁做的还得
  再去用户表里一个个查。已在 `AuditService.List` 里按 ID 去重后补齐邮箱，
  补不上的（用户已删、外键置空）留空由前端显示「(已删除)」。

- **`AccountFilter.Normalize()` 会把 `Limit` 压到 `MaxAccountPageSize`(200)**，所以
  「填一个大 Limit 一次取全量」这个写法是无效的。批量刷新的 `selectAccounts` 原本
  正是这么写的（`Limit = maxBatchAccounts` = 5000），结果账号超过 200 个时，
  「刷新全部」只刷前 200 个——而任务上写着的总数也是 200，界面上完全看不出少了。
  要全量就得翻页（`RefreshService.collectAccounts`，同 `AccountService.collectAccounts`）。

## 一次技术债清理（2026-08-20）

- **删掉 8 条没有任何调用方的 sqlc 查询**（`GetTenantQuota` / `GetTenantBySlug` /
  `DeleteTenantMember` / `DeleteRefreshLogsBefore` / `FindAccountIDByAlias` /
  `DeleteMailAccountTagsByAccount` / `ListMailAccountsByGroup` /
  `ListMailAccountsByRefreshStatus`）与对应的 4 个 repo 方法，生成代码少了约 530 行。
  这些是「先把可能用到的查询写好」留下的，而 P5/P6 后来整个被删掉了。
- **`AccountFilter` 删掉 `TagIDs` / `Untagged` 两个字段**：handler 不填、repo 不读，
  留着等于承诺了一种并不存在的筛选——下一个人照着传参会得到「筛选没生效」且无从排查。
- **删掉 `api/platform_test.go`**：它是 P0 时期的脚手架，自己挂一条假的
  `/admin/ping` 来验证守卫，文件里的注释就写着「P3 落地后每个端点还要各有一条」。
  P3 的 `TestNonAdminIsRejectedOnEveryAdminEndpoint` 已经逐条覆盖 23 个真实端点。
- **删掉 5 条低作用用例**：断言纯函数「跑十次结果一样」、断言 SHA-256 就是 SHA-256、
  `n=0` 时不扣配额、空配置下不提权、空回退链的错误分类。它们都不会红。
- 前端删掉从未被引用的 `lib/utils.ts`（`cn`，Kumo 自带一个）与 `api/system.ts`
  （健康检查，前端从来没调过），连带卸掉 `clsx` 与 `tailwind-merge` 两个依赖。
- **`errcheck` 配了 `check-blank: true`**，所以 `_ = f()` 也会被拦——
  原先那种 `if err := f(); err != nil { _ = err }` 的写法是为了绕过它，
  读起来却像漏写了处理。已改成 `//nolint:errcheck` 加一句「为什么故意丢弃」。
- `go mod tidy`：go-imap 与 go-sasl 之前被标成 indirect，实际是 imapx 的直接依赖。
- `.env.example` 与 `docs/configuration.md` 补上 `JOB_WORKERS` /
  `JOB_ACCOUNT_DELAY_MS` / `JOB_EVENT_RETENTION_DAYS`——代码里读了，文档里一直没写。

### 分组压平成一层（2026-08-27）

分组树是照着 outlookEmail 的老界面搬来的，但层级在这个产品里没有对应的用法，
带来的全是要向用户解释的规则：新建分组时先选「上级分组」、旁边标一句
「分组树最多三层」、账号数分「直属 / 含子树」两个口径、删除还要交代子分组会一起消失。
功能没多，规则先多了一圈——用户要的只是把账号分堆。

`parent_id` / `level` 两列随 `000011_flat_groups` 删除（SQLite 去不掉列上的自引用外键
与 `CHECK (level IN (1,2,3))`，只能整表重建，沿用 000008 的 no-transaction 写法）。
原来的子分组一律变成顶级分组，账号仍挂在原来的分组上。

压平真正会丢的不是行，而是**继承**：子分组没配代理时走的是父分组的代理，
父子关系一没，这批分组下的账号会悄悄从代理出口变成直连——轻则被风控，
重则暴露真实地址。所以 `000010_group_proxy_pushdown` 先把生效的那一份代理
（三列一起，同 `mailer.ResolveProxy` 的「整组一起取」）写进子分组自己身上，
再做结构变更。`migrate_test.go` 里有一个造三层树的用例专门守这两件事。

接口上少了 `POST /groups/:groupID/move`，`GET /groups` 从树改成列表
（`children` / `total_account_count` 一并去掉）。`GroupTree.tsx` 换成 `GroupList.tsx`。

### 对外取件 API（2026-08-27）

以前想用脚本取件只有一条路：拿登录接口换 Cookie，然后用会话冒充用户。
那等于把一个人的全部权限交出去——包括导出全部凭据明文。

现在有 Key 了，但**没有第二套接口**：Key 是一个只读的虚拟角色
`model.TenantRoleAPI`，鉴权通过后走的还是 `/mail/**` 那一份路由，
权限由现有的 `middleware.Require` 收敛。整个改动没有新增一个业务 handler。

三处改动串起来：

| 位置 | 做什么 |
|---|---|
| `middleware/session.go` | Cookie 优先；没有 Cookie 才看 `Authorization: Bearer`，命中则记 `actor_kind=api_key` |
| `middleware/tenant.go` | Key 不查 `tenant_members`（它不属于任何用户），只校验 URL 的 tenantID 与 Key 绑定的一致，然后交出一个虚拟成员 |
| `model/permission.go` | `TenantRoleAPI` 只有 group/account/message 的 read。`tenant_members.role` 的 CHECK 是 `('owner','admin','member')`，这个取值不可能从库里读出来 |

Key 因此**读不到也重置不了自己**（那两个端点要 `tenant:update`），泄露后不能自我续命；
导出接口要 `mail:account:secret`，同样拿不到。

`token_hash` + `token_enc` 两列存两份：前者鉴权按 O(1) 命中，后者只为「回到页面
还能看见自己的 Key」。代价是拿到库 + `ENCRYPTION_KEY` 就能还原它——这个库里本来
就躺着同样可解密的 refresh_token 与邮箱密码，不新增风险类别。

`/llms.txt` 公开、不含 Key，由 `handler.APIEndpoints` 渲染。`TestLLMsTxtMatchesRoutes`
把文档里的每条路径拿去和 Echo 路由表比对：文档漂移比没有文档更糟，Agent 会照着一条
不存在的路径反复重试。

### 文档全量对账（2026-08-27）

分组压平与 API Key 两件事之后把 `AGENTS.md` / `README.md` / `docs/` 全部过了一遍，
按 [README §当前状态](README.md) 的约定「设计文档要与现实同步」逐条修。除了同步这两个
功能之外，这次对账**翻出来四处早就漂移的地方**——都不是这两次改动造成的：

| 位置 | 问题 |
|---|---|
| `05 §8 OAuth 助手` | 当时三个端点确实从未实现，曾先把文档纠正成“未实现”；2026-08-28 已以一次性 state + PKCE 的新端点闭环，见本文末尾进度 |
| `05 §10.4` | `GET /admin/jobs` 同样不存在。任务是按租户跑的，管理员从 `/admin/tenants/:id/mail/jobs` 进去即可 |
| `06 §4 状态管理` | 列了 7 个 store，实际只建了 4 个。`mailGroupStore` 等 5 个从来没建——数据只有一个页面用，放全局只是多一份要手动失效的副本 |
| `AGENTS §5.1` / `05 §1.2` / `response.go` | 都写着「1001 的 `data` 带 `{metric, limit, used}`」，而 `mailError` 实际回的是 `data: null`，上限与用量在 `message` 里。三处一起改成现状 |

另外两处小的：`06 §8` 说类型在 `web/src/types/mail.ts`（实际在 `web/src/api/mail.ts`）、
`.env.example` 说 `BOOTSTRAP_ADMIN_USERNAME` 认「邮箱」（000008 之后认的是用户名）。

**同批删掉了 `GET /api/v1/admin/tenants`**：`AdminTenantsPage` 删除之后它就没有调用方了，
和 000006/000007 清掉的那批是同一类东西。一次删干净——路由、`AdminHandler.ListTenants`、
`AdminService.ListTenants`、`Store.ListAdminTenants`、两个方言的
`ListAdminTenantsPage` / `CountAdminTenants`、`model.AdminTenant` 与 `AdminTenantFilter`、
前端的 `adminApi.tenants()` 与 `AdminTenant` 类型。

删之前先把它带走的两条断言搬了家，这是这次唯一需要动脑的地方：

- `TestAdminTenantsParity` 是**唯一**守着「`COALESCE(tenant_quotas.max_accounts, plans.max_accounts)`
  顺序」的用例——写反的话管理员调低配额完全不起作用，而界面上还显示着调低后的数字。
  用户列表的 SQL 里有同一段 COALESCE，断言因此并进 `TestAdminUsersParity`
- `TestLoweringQuotaBlocksNewAccountsButKeepsExisting` 用租户列表验证超额标记可见，
  改成查 `/admin/users`——那才是管理员真正会看的那份清单

### 顺带修掉一个会随机变红的加密用例（2026-08-27）

`TestDecryptRejectsCorruptedCiphertext` 的「篡改内容」分支原来是**翻转 base64 串的
最后一个字符**。`RawURLEncoding` 的最后一个字符可能只有 2 或 4 位有效，其余是填充位，
而 Go 默认的 base64 解码器**不校验填充位**——改到填充位上时解出来的字节一模一样，
解密当然成功，用例就红了，而被测代码没有任何问题。

跑 `go test -count=1` 时约一半概率失败（平时被测试缓存盖住了）。改成解码后翻认证标签
最后一个字节的一位，必被 GCM 拒绝。一条会随机变红的安全断言比没有更糟：
它会先被当成噪音忽略，然后有一天真的坏了也没人信。

### 配额口径调整与两处删除（2026-08-27）

**令牌刷新不再有额度**（`000013` 删掉 `plans` / `tenant_quotas` 的 `daily_token_refresh`）。
刷新令牌是「账号还能不能用」的前提：卡住它，用户看到的不是「今天少刷一点」，而是一批账号
集体登录失败——那个后果比省下的上游调用重得多。真正要防的「批量刷把服务商打到风控」，
靠的是任务系统的并发数与账号间隔（`JOB_WORKERS` / `JOB_ACCOUNT_DELAY_MS`），
不是一个每天清零的计数上限。

用量仍然记：新增 `quota.Service.Record`（只记账、不判上限），与 `CheckAndConsume` 分开是
有意的——一个叫 Check 的函数如果在某些指标上从不拒绝，读代码的人会以为那里有限额。
用量页上「今日刷新令牌」因此显示为一个数字 + 「不限」徽章。

**取件额度维持一视同仁**。中途考虑过「网页取件不计入、只限 API」，写到一半推翻了：
会话 Cookie 同样能写进脚本，只限 API 等于留下一个「逆向网页就能绕开」的口子。
网页、API Key、管理员因此共用同一个 `mail_fetch` 计数器与同一条上限。

**删掉 `users.avatar_url`**（`000014`）：个人资料页有一个「头像 URL」输入框，
但界面上**没有任何一处会显示它**——左栏、成员列表、后台用户列表用的都是用户名首字。
和 000006 清掉的那批 `allow_*` 开关是同一类东西。

**删掉左栏的「导入」入口**：`/mail` 的工具条上已经有「导入邮箱」，两个入口指向同一页。
路由 `/mail/import` 保留。

#### 顺带修好一个测不准的迁移用例

`TestNoTxMigrationsAreRepeatable` 原来是在**跑完全部迁移**的库上重跑每个 no-tx 文件。
000014 删掉 `avatar_url` 之后它立刻红了：000008 的整表重建当然找不到那一列。

但那是个不会发生的场景——迁移是有序的，000008 只可能在版本 7 的库上执行。用例改成
「为每个 no-tx 迁移单独建库、跑到它自己那一版、抹掉版本号再跑一次」，
这才是「文件跑完、版本号还没记上」时崩溃的那个窗口，顺带把外键检查也加上了。

### 导出去掉二次密码验证（2026-08-27）

原设计照搬 outlookEmail 的 `export_verify_tokens`：导出前再输一遍自己的登录密码。
取消它是产品决定——这个平台的用户就是来批量取自己凭据的，导出是常规操作而不是危险动作，
每次拦一道密码框，换来的主要是「把密码练成肌肉记忆」。

**剩下三道闸门一件没松**：独立权限 `account:secret`、强制审计（路由上的 `AuditWrite`）、
按用户 10 次/分钟限流。真正的差别在于：密码框防的是「会话被盗用/电脑没锁屏」这一类，
而那两件是事后能追责、事中能限速的。取消之后这类场景的暴露面确实变大了，
这一点写进了 05 §4.4 与 AGENTS §5.4，不留给下一个人自己去发现。

`AccountHandler` 因此不再需要 `AuthService`（它只为这一处密码比对而存在），
构造函数收回到一个参数；`AuthService.VerifyPassword` 与 `ErrPasswordMismatch`
一并删除——那是它们唯一的调用方，留着就是下一个「点了不生效的开关」。测试 `TestAccountExportVerifiesPasswordAndRoundTrips`
改名为 `TestAccountExportRoundTripsAndAudits`，去掉密码分支，保留 round-trip 与审计断言。

### 启动时自动建出管理员（2026-08-27）

原来的 `BOOTSTRAP_ADMIN_USERNAME` 只**提权**，不建号。于是一个刚
`cp .env.example .env && make dev` 的人只能看到一行 WARN 说后台进不去，
而正确操作是「先注册 → 改配置 → 重启」这三步——它在任何界面上都不明显。

现在多了 `BOOTSTRAP_ADMIN_PASSWORD`：用户不存在且填了密码时，启动直接把他建出来
（绕过 `REGISTRATION_MODE=closed`，这是部署者在自己机器上开门，不是公开注册）。

两个刻意的边界：

- **用户已存在时不碰密码。** 每次启动都按配置重置的话，配置文件就成了一个能悄悄接管
  已有账号的入口，而且用户改完密码会在下次重启被静默改回去
- **建号失败不阻断启动**：其余用户照常能用，只是后台进不去，日志里有 ERROR

实现上把注册的「用户 + 个人工作空间 + 成员 + 默认分组 + 配额」五件套抽成了
`AuthService.createAccount`，注册与引导共用。抄第二遍迟早会漏掉其中一件，
而漏掉任何一件的用户登录后页面就是坏的，且无法自助修复。
`TestBootstrapAdminCreatesUserWhenPasswordGiven` 逐项断言这五件都在。

`.env.example` 同时补了一个**示例 `ENCRYPTION_KEY`**（解开是
`example-key-do-not-use-in-prod!!`），让 `cp .env.example .env` 之后不再报
「凭据将以明文存储」。它和那个 admin 密码一样是公开的，两处都写明了必须替换。

### 文档瘦身（2026-08-27）

删掉 5 篇：`01-analysis`（老 Python 项目的现状分析）、`02-architecture`（目标架构）、
`07-roadmap`（分阶段路线图与验收）、`09-ui-revamp`、`10-linear-redesign`（两次改版的过程记录）。

判断标准是**「它描述的是当前系统，还是当时的打算」**：前者要跟着代码一起维护，
后者写完就定稿了。留着一份定稿的计划，下一个人照着它写代码就会写出一个不存在的系统——
文档漂移比没有文档更贵。留下的是 03/04/05/06/08 与 PROGRESS，它们回答的都是
「今天它怎么工作」。

删之前把只存在于那几篇里的东西搬了家：

- `go.mod` 里的 `lib/pq` **不是数据库驱动**（连接走 pgx），只为 sqlc 生成的
  `pq.Array` 包装而存在 → 并进 03 §5.1
- 测试口径（只测核心行为与安全边界、并发断言要做变异验证）→ `AGENTS.md` §7 本来就是全文
- 「明确不做的事」清单与 API Key 的重做对照 → 并进 `docs/plan/README.md`
- Linear 视觉语言的三条硬规则 → `AGENTS.md` §6.2 本来就有

同时删掉根目录的 `email.png`：那是另一个产品（小苹果 Pro 邮箱助手）的界面截图，
不是本项目的，放在仓库里迟早被谁当成我们的宣传图用出去。

### 令牌刷新收归令牌页，并支持按分组（2026-08-27）

邮箱页左栏顶上那个「批量刷新令牌」大按钮删了。它其实只是一个跳去 `/mail/tokens`
的链接，但长得像一个动作按钮——用户会以为邮箱页上的它和令牌页上的「刷新全部」
是两件不同的事。刷新令牌的全部界面（范围选择、进度、失败原因）都在令牌页，
入口也只留全局导航里的那一个。

令牌页补上第三条范围：**按分组刷新**。账号多起来之后「全部」要跑十几分钟，
而用户通常只想刷刚导入的那一批，分组就是他们区分批次的方式。
后端加 `scope=group` + `group_ids`（`RefreshScopeGroup`），分组先校验归属再取账号——
不校验的话，拿别人的 `group_id` 提交就成了一个「这个分组下有没有账号」的探测口，
`TestBatchRefreshRejectsForeignGroup` 守着这条。

顺带修了「刷新全部」只覆盖前 200 个账号的 bug，成因见上面坑列表里的
`AccountFilter.Normalize`。

另外，邮箱页点邮箱地址打开右侧收件箱之后**没有办法关闭**：那一栏唯一的关闭按钮
是移动端的返回键（`md:hidden`），宽屏下根本不渲染。现在点已经打开的那个邮箱
就收起（`aria-expanded` 表达这个状态），桌面端也补了一个显式的关闭按钮。

### 配色改回 Cloudflare（2026-08-27）

`style.css` 里那一整块覆盖 `--color-kumo-*` 的 Linear 色板删掉了：品牌 #5e6ad2（薰衣草）、
带蓝调的近黑 canvas #010102、正文 #d0d6e0，一条不留。**Kumo 就是 Cloudflare 的设计系统**，
它的默认令牌即是 Cloudflare 的配色，我们在上面压一层自己的色板，等于把它改成了别人家的样子。

改完之后照 Cloudflare 的分工（值都在 `node_modules/@cloudflare/kumo/dist/styles/theme-kumo.css`）：

- **中性色是纯灰**（`oklch(… 0 0)`）。Linear 那套灰是带蓝调的，这一条是「像不像 Cloudflare」
  最直观的区别。
- **Kumo 的按钮从来就没有渐变**：它的 primary 只是对品牌色
  （`--color-kumo-brand`，构建产物里解析为 `#056dff`）做 `color-mix` 派生 hover/ring，
  实心一块。那道蓝紫渐变是我们自己加的。（这一版之后连实心按钮也不用了，见下一节。）
- **橙色 `#f6821f` 只留给品牌标识**（左上角那个信封小方块，配 `#fbad41` 做 Cloudflare
  商标那对橙的渐变）。它当不了按钮底色：橙配白字对比度 2.8:1，达不到 WCAG AA 的 4.5:1，
  Cloudflare 官网上的橙色 CTA 配的也是深色文字。左栏头像圆点原来是品牌蓝配 11px 白字
  （4.2:1，同样不够），一并换成中性底 + 正文色。

分组圆点与状态点的自有令牌（`--color-ebx-*`）也换成了 Kumo 语义色的字面值。
写字面值而不是 `var(--color-kumo-success)` 是有原因的：Tailwind v4 会摇掉没有被任何
工具类**直接**引用的 `@theme` 变量，转一道手就可能在生产构建里变成一个透明的点。

### 按钮统一成一种长相（同日，用户要求）

换完色之后暴露出一个更根本的问题：一排平级动作里，「导入邮箱」是实心蓝、旁边的
「导出 / 刷新 / 启用 / 停用」全是白底描边。用户的原话是「都是白色按钮就统一用白色，
为什么一定要使用不同颜色的按钮呢」「我讨厌这种对比度」。

于是**全站按钮只剩一种长相**：`variant="secondary"`（白底 + 一圈 hairline），
危险动作 `variant="secondary-destructive"`（同样的白底，红字）。
17 个 `primary` 和 4 个实心 `destructive` 全部换掉，包括登录提交、首页 CTA、
弹窗里的「保存 / 确认导出」。强调改为靠 `size="lg"` 和留白表达。

这也顺带解决了三处手搓按钮的问题：它们原本是 `bg-kumo-brand` + `text-white`，
缺了 Kumo 给 primary 加的那层 `color-mix`，和真正的 primary 并排时深浅差半档——
现在没有实心按钮了，这个坑也就不存在了。品牌蓝因此只剩两个出场：链接文字和 focus ring。

### 登录后增加资源推荐页（2026-08-28）

新增 `/resources`，从左侧次级导航进入，按网络与部署、批量业务工具、其他服务分组展示。
推广关系在页面顶部统一说明，同时在具体资源上逐项标记；外链使用新标签页并带
`rel="sponsored nofollow"`。README 的推广披露也从末尾免责声明移动到资源列表之前，
让说明与链接同时出现，而不是让读者点完链接后才在页面底部看到。

### Microsoft OAuth 重新授权闭环（2026-08-28）

Token 页最初只列 `auth_failed` / `consent_required` 的 Outlook 账号并提供重新授权；
2026-08-30 扩展为展示所有刷新失败类型的具体原因，仅认证/权限类保留重新授权动作。
后端用授权码 + PKCE、随机一次性 state 和 10 分钟流程表完成交换；refresh token 只以密文
存在服务端，先用 Graph `/me` 核对主邮箱或别名，再实际刷新一次，全部成功后才用窄 UPDATE
替换 `client_id` / `refresh_token` / `auth_channel`。因此授权页面选错账号、权限不足或网络失败
都不会破坏账号原有凭据。

默认暂用参考项目的 Microsoft client ID 与 localhost 回调，同时保留粘贴最终跳转地址的
兼容流程；配置自己的 HTTPS callback 后会自动回到 Token 页完成。两个端点复用用户侧与
管理员侧同一路由表，要求 `account:write`，API Key 与只读成员进不去；公开 callback 仍以
state 中的 tenant ID 做带租户条件的查询。`TestOAuthReauthorizationOnlyReplacesVerifiedCredentials`、
`TestOAuthIdentityMismatchKeepsOldCredential`、跨租户 API 用例和双引擎 parity 用例守住这些边界。
访问日志对公开 callback 单独去掉查询串，避免把短期授权码和 state 写进日志。

### 镜像发布迁移到 GHCR（2026-08-28）

`.github/workflows/build.yml` 从 Docker Hub 改为 GitHub Container Registry：工作流增加
`packages: write`，用仓库内置的 `GITHUB_TOKEN` 登录 `ghcr.io`，不再保存长期 Docker Hub
凭据。镜像名从 `$GITHUB_REPOSITORY` 生成并显式转成小写，避免带大写字母的 GitHub owner
生成 Docker 不接受的镜像引用。

构建监听 `v*` tag push，与 `release.yml` 并行执行：一个创建 Release，一个构建并推送镜像，
不依赖 Release 事件的二次触发，因此两个工作流都使用仓库内置的 `GITHUB_TOKEN`，省去
`PAT_TOKEN`。每个版本只推对应的版本号 tag。

### 分组代理终于有了界面入口（2026-08-31）

后端从一开始就收 `proxy_url` / `fallback_proxy_url_1` / `fallback_proxy_url_2`
（`CreateMailGroupRequest` / `UpdateMailGroupRequest`，加密入库、`mailer.ResolveProxy`
在账号没配代理时回落到它），但 `GroupFormDialog` 只有名称、描述、颜色三项，
`mailApi.createGroup` / `updateGroup` 的参数类型里连这三个字段都没声明——
也就是说整个 web 前端没有任何写代理的地方，只能自己 `curl` PATCH。
账号那侧同样：`AccountDrawer` 只读 `proxy_url_masked`，没有编辑入口（本次未处理）。

补入口时真正要定的是「编辑框里放什么」。打码串不行：用户进来改个名字一按保存，
`socks5://u:****@host` 就被当成口令写回库里，代理从此是坏的，而界面上一切正常，
直到某个账号取信失败才会发现。06 文档 §6.2 本来就写着「代理 URL 在输入框以外
一律显示打码版」——言下之意输入框里该是明文，只是没人实现。

于是加了 `GET /mail/groups/:groupID/proxy` 单独送明文。它是读操作，
但收敛口径跟着「读走了什么」而不是「谁在读」走，因此按导出同一档：
`account:secret` 权限（API Key 天然没有这一项）＋ `AuditWrite` 而不是
`AuditAdminRead`——普通用户取走一条代理口令同样要留痕，`AuditAdminRead` 只记管理员。
没配限流：一次只出一个分组的三条代理，分组数本身受配额约束，与「一次取走整租户凭据」
的导出不是一个量级。明文不进列表接口，否则一进 /mail 就把全部分组的代理口令
发到浏览器，而绝大多数时候没人要看它们。

两个容易写错的地方，各钉了用例：

- **解密失败必须报错，不能回落。** `maskStoredProxy` 解不开时回显「(无法解密)」是对的
  ——那是只读展示，少显示一行没有后果。但这个端点的返回值会落进输入框再随下一次保存
  写回库里，回落成空串更糟：空串和「本来就没配代理」在表单上长得一模一样。
- **明文没到手时，保存要整组省掉代理三项。** `UpdateMailGroupRequest` 的指针语义
  （不传即保持原值）就是为这个场景准备的；照发一组空输入框等于把代理静默清掉，
  那批账号从此走服务器公网 IP 直连。前端的 `proxyLoad !== "ready"` 分支守这一条，
  `TestGroupProxyRoundTrip` 与 `GroupFormDialog.test.tsx` 两侧各测一半。

表单里还顺手提示了两件靠读代码才知道的事：`ResolveProxy` 在主代理为空时视整组未配置
（只填备用 = 静默直连），以及 `NewDialer` 只支持 socks5/socks5h，http 代理仅 Graph 通道可用。
