# 07 · 实施计划

## 0. 阶段总览

| 阶段 | 名称 | 主要交付 | 依赖 |
|---|---|---|---|
| P0 | 地基改造 | 模块改名、加密设施、Kumo 接入、SaaS 数据模型与注册流程、平台管理员中间件、双引擎 SQL 基建 | – |
| P1 | 分组与账号 | 分组树、账号 CRUD、批量导入/导出、别名、代理配置、配额强制 | P0 |
| P2 | 邮件协议层 | `pkg/mailer` 全量：Graph + IMAP + 回退链 + 代理；邮件读取/详情/附件 | P0 |
| P3 | 管理后台 | 用户管理、跨租户邮箱管理、套餐与配额管理、审计 | P1 |
| P4 | 任务系统 | jobs 表、worker pool、SSE、Token 批量刷新、刷新统计与日志 | P1, P2 |

**P4 之后即为完整交付范围。** 原先规划的转发与调度、对外 API / API Key / 分享链接 /
本地邮件保留、临时邮箱等增强能力**已从方案中移除**，相关设计不再保留在本套文档里
（见 §5「已移出范围」）。

**并行机会**：P1 与 P2 无相互依赖（一人做数据/CRUD，一人做协议层）；
P3 与 P4 也可并行（一人做后台，一人做任务系统）。

---

## P0 · 地基改造

**目标**：让模板具备承载 SaaS 邮箱业务的前置条件。这阶段不产出终端用户可见的邮箱功能，
但每一项都是硬阻塞。

### 1. 基础清理

1. **模块改名** `go-react-template` → `emailbox`
   - `go.mod` + 全仓库 import 路径；`web/package.json` 的 `name`
   - Makefile 里 `docker build -t go-react-template` 同步改
2. **`pkg/crypto`**：AES-256-GCM Encrypt/Decrypt（`enc:v1:` 前缀）、`HashToken`
   - `configs`：`ENCRYPTION_KEY`、`APP_ENV`；生产模式缺 key 直接 fatal
3. **HTTP 装配修正**（[05 文档 §2](05-api-design.md)）
   - 导入类路由的 `BodyLimit` 覆盖 + 一个回归测试固定该行为
   - Gzip `Skipper` 跳过 SSE 路径
   - SSE handler 取消写超时的封装 `handler.SSEWriter`
   - 核查会话 Cookie 的 `SameSite`，决定是否需要 CSRF token

### 2. SaaS 基础（[08 文档](08-saas-admin.md)）

4. 迁移 `000002_saas`：`users.platform_role`、`tenants.kind`、
   `plans` / `tenant_quotas` / `usage_counters`，sqlite + postgres 两套
5. **改造 `AuthService.Register` 为事务化五件套**：
   user + 个人租户 + tenant_member(owner) + 默认邮箱分组 + tenant_quotas
   - 必须有测试断言「中途失败则全部回滚」——留下没有租户的用户会导致该账号永久不可用
6. **`middleware/session.go` 补 `users.status` 校验**：非 active 的用户会话立即失效。
   模板目前没做，是禁用功能的前提
7. `middleware/platform.go`：`RequirePlatformAdmin`
8. 启动时的 `BOOTSTRAP_ADMIN_USERNAME` 提权逻辑 + 无管理员时的 WARN 日志
9. `pkg/quota`：生效值计算（`COALESCE(override, plan)`）、`CheckAndConsume`
10. 权限模型扩展：`pkg/model/permission.go` 新增 10 个租户级权限 + 角色映射（[03 文档 §6](03-data-model.md)）

### 3. 双引擎 SQL 基建

11. `sqlc.yaml` 的 `schema` 从单文件改为目录（`db/migrations/sqlite`、`db/migrations/postgres`）
12. 用 `000002_saas` 的查询跑通一次 `make sqlc-generate` → `make sqlc-verify`
13. **落地 [03 文档 §5.1](03-data-model.md) 的双引擎写法**：
    SQLite 用 `json_each(?)`，PostgreSQL 用 `= ANY($1::text[])`，
    repo 层吸收差异，方法签名一致
14. **跨引擎对照测试脚手架** `pkg/repo/parity_test.go` + CI 加 `postgres` service container；
    本地无 PG 时自动跳过并打印原因

### 4. Kumo 前端接入（[06 文档 §1](06-frontend.md)）

15. `bun add @cloudflare/kumo @phosphor-icons/react`
16. `web/src/style.css` 改造：Kumo 两行放在 `@import "tailwindcss"` **之前**；
    删除 shadcn 风格的 `@theme inline` 令牌块与 `@layer base` 里写死的颜色
17. 删除 `web/components.json`；`lucide-react` → `@phosphor-icons/react` 并移除 lucide 依赖
18. 把现有 5 个页面（Home/Login/Register/Dashboard/Layout）改用 Kumo 组件与语义令牌，
    作为团队的样板参考
19. ESLint 加规则拦截原始 Tailwind 色类与 `dark:` 前缀
20. `.github/workflows/ci.yml` 的 Node 版本对齐到 22+（Kumo 为 ESM-only）

### 验收

- `make lint && make test && make build` 全绿
- `pkg/crypto` 单测：加解密往返、密文格式、错误密钥解密**报错而非静默返回空**
- 注册一个新用户 → 自动有个人租户、默认分组、配额记录；登录后 `active_tenant_id` 已设置
- 管理员提权生效；非管理员访问 `/api/v1/admin/*` 返回 403
- 禁用某用户后，其已登录的浏览器下一次请求即失效
- `parity_test` 在 SQLite 与 PostgreSQL 上跑出一致结果
- 改造后的 5 个页面在亮/暗模式下均正常（不写任何 `dark:`）

---

## P1 · 分组与账号

### 任务

1. 迁移 `000003_mail_core`（groups / accounts / aliases），sqlite + postgres
2. `db/query/` 两套 SQL + `make sqlc-generate`；账号列表的多条件筛选按 §5.1 双引擎写法落地
3. `pkg/model`：`MailGroup`、`MailAccount`、各 DTO
4. `pkg/repo`：groups.go / accounts.go / aliases.go
   - 批量取别名用 `sqlc.slice` / `= ANY` 一次取回，避免 N+1
   - `parity_test` 覆盖账号筛选的全部条件组合
5. `pkg/service`
   - `GroupService`：树查询、层级校验（≤3）、防环移动、级联删除、排序
   - `AccountService`：CRUD、别名校验、**批量导入解析**（[04 文档 §7](04-mail-protocol.md)）、批量操作、导出
   - 凭据字段进出库经 `crypto` 加解密
   - **配额强制**：`max_accounts` / `max_groups` 在创建路径检查；
     导入超额部分计入 `skipped` 而非整批失败
6. `pkg/handler` + 路由注册（[05 文档 §3、§4](05-api-design.md)）
7. 前端：`/mail` 左两栏（`GroupTree` + `AccountList`）、`/mail/import` 向导、
   `/mail/groups` 分组管理、`/settings/usage` 配额页
   - `selectionStore` 与 `AccountBatchMenu`（Kumo `DropdownMenu`）
   - 自建 `VirtualList`（`@tanstack/react-virtual`）与 `SplitPane`

### 验收

- 配额为 50 的租户导入 200 行 → 创建 50、跳过 150，响应明确说明原因
- 分组三级限制、防环、级联删除、账号回落默认分组，均有单测
- 别名冲突的 4 种情形均被拒绝
- 导出文件能被本平台重新导入且账号数一致（round-trip 测试）
- 账号列表 1 万条下前端滚动不掉帧
- **跨租户测试**：A 用户带 B 用户的 accountID 请求 → 404

---

## P2 · 邮件协议层

**技术风险最高的阶段。** 建议单独开分支，先把 `pkg/mailer` 做成可独立运行的
CLI 调试工具（`cmd/mailprobe`），用真实账号验证后再接入 service。

### 任务

1. `pkg/mailer` 骨架：接口、领域类型、provider 表、错误分类（[04 文档](04-mail-protocol.md) §1、§2、§9）
2. `proxy.go`：解析优先级、`{mail}` 展开、failover 候选、HTTP Transport 与 IMAP Dialer 构建
3. `graph/`：token scope 三级降级 + AADSTS 判定、列表/详情/附件/标已读/删除、`$batch`、429 处理
4. `imapx/`：XOAUTH2 与密码鉴权、IMAP ID、UTF-7 编解码、文件夹解析、UID/序列号、
   `BODY.PEEK` 分页抓取、MIME 解析（含 GBK/Big5 头解码）
5. `chain.go`：回退链、不可回退错误判定、成功通道写回
6. `cmd/mailprobe`：逐通道输出诊断（对标 `outlook_mail_reader.py` 的三通道测试脚本）
7. 接入 service：`MessageService`，路由注册（[05 文档 §5](05-api-design.md)）
   - `daily_mail_fetch` 配额在走远端前 `CheckAndConsume`
8. 前端：`/mail` 右两栏（邮件列表 + 详情），`MessageBody`（DOMPurify + sandbox iframe）

### 验收

下表是协议层要覆盖的场景。**不要求逐条用真实账号手工跑完**——真账号拿不齐（Gmail /
163 / QQ 各要一个可用账号加代理），而一次性的人工验收也不会留在 CI 里。
能用进程内服务器（`httptest` / `imapmemserver`）复现的直接写成常驻测试，
剩下的等手边正好有对应账号时顺手验一次即可。

| # | 场景 | 期望 |
|---|---|---|
| 1 | Outlook OAuth 正常账号 | Graph 通道成功，`auth_channel=graph` 写回 |
| 2 | Graph 权限不足的账号 | scope 降级到 `read` 或 `.default` 后成功 |
| 3 | Graph 完全不可用 | 回退到 `imap_new`；再不行到 `imap_old` |
| 4 | 已被封账号 | 返回 `banned`，**不继续回退**，账号状态置 banned |
| 5 | refresh_token 失效 | 返回 `auth_failed`；Graph 上继续回退到两条 IMAP，IMAP 上不再回退（04 文档 §3） |
| 6 | Gmail 应用密码 | IMAP 密码鉴权成功，垃圾箱走 `[Gmail]/Spam` |
| 7 | 163/126 | IMAP ID 发送成功，无 `Unsafe Login` |
| 8 | QQ 邮箱垃圾箱 | UTF-7 名 `&V4NXPpCuTvY-` 能 SELECT |
| 9 | 中文主题 + GBK 编码头 | 主题正确解码不乱码 |
| 10 | 带附件邮件 | 附件列表正确、单个下载与 ZIP 打包都可用 |
| 11 | 配置 SOCKS5 代理 | 出站走代理；主代理故障时自动切 fallback |
| 12 | `{mail}` 模板代理 | 不同账号使用不同代理身份 |
| 13 | **20 个账号并发拉信** | **真正并发**（对比 Python 的串行），无全局锁 |
| 14 | 分页 skip=40&top=20 | Graph 与 IMAP 结果一致且无重叠/遗漏 |
| 15 | 恶意 HTML 邮件 | iframe 隔离生效，脚本不执行，远程图片默认阻断 |

第 13 项要专门测——它是本方案相对 outlookEmail 的核心价值主张。
若因某处引入全局状态而退化成串行，等于白做。

---

## P3 · 管理后台

### 任务

1. `pkg/service/admin_service.go`：用户列表/详情/改状态/改角色/重置密码/删除
   - 禁用与重置密码时清空该用户全部 sessions
   - 最后一个管理员不可降级/删除
   - 删除用户时**物理清除凭据密文列**（[08 文档 §6](08-saas-admin.md) 第 6 条）
2. 套餐管理：plans CRUD、租户配额覆盖（必须带 `note`）
3. 跨租户邮箱路由：`/admin/tenants/:tenantID/mail/**` 复用 P1 的 handler
4. 审计：管理员的三类读操作（看账号列表、看邮件正文、导出）单独记 `audit_logs`
5. `/admin/stats` 平台概览
6. 前端：`/admin` 总览、`/admin/users`、`/admin/plans`、`/admin/audit`、
   `/admin/tenants/:id/mail`（复用 `/mail` 组件，顶部常驻管理员 Banner）
   - `AdminRoute` 守卫；`mailBase()` 的 scope 切换（[06 文档 §7](06-frontend.md)）

### 验收

- 非管理员访问任何 `/admin/*` 端点均 403（**每个端点一条测试**）
- 管理员在 B 租户下的操作，`audit_logs` 里 `actor_user_id` 是管理员本人、`actor_kind='admin'`
- 禁用用户后其会话立即失效；启用后可重新登录
- 调低某租户配额后，该租户新增账号被拒，已有账号不受影响，后台显示超额标记
- `mailBase()` 单测：admin scope 不会误打到管理员自己的租户路径

---

## P4 · 任务系统与 Token 刷新

### 任务

1. 迁移 `000005_mail_ops`（jobs / job_items / job_events / mail_refresh_logs）
   —— `audit_logs` 是 P3 的硬依赖，已随 `000004_platform_admin` 先行落地
2. `pkg/job`：Manager（提交/查询/停止/心跳）、worker pool、事件写入
   - 启动时把心跳超时的 running 任务标为 `interrupted`
3. SSE：`stream.go` 广播 + `job_events` 回放（`Last-Event-ID` / `?last_event_id=`）
4. `RefreshService`：单个刷新、批量提交、写回 `refresh_token_enc` + `last_refresh_*`
   - **refresh_token 轮换**：微软返回新 token 时必须持久化，漏了会导致下次刷新失败
   - `daily_token_refresh` 配额在提交时按 `len(account_ids)` 预扣
5. 统计接口：按 `error_kind` 聚合
6. 前端：`/mail/tokens`（Kumo `Table` + `Pagination` + `Meter` 进度 + `Code` 日志）+ `jobStore`

### 验收

- 批量刷新：进度实时、可停止、刷新页面后进度可恢复（不做大批量压测，
  几十个账号足以覆盖这三条行为；真实规模下的表现由配额天然限速）
- 强杀进程后重启：僵尸任务被标为 `interrupted`
- 失败分类统计正确（banned / auth_failed / proxy_failed 各归各类）
- 并发写 `jobs` 计数无竞争（`go test -race` 覆盖）
- 任务只能被所属租户（或管理员）查看

---

## 1. 测试策略

**总原则：测核心行为，不做防御性堆砌。** 一条用例要么钉住一个真会出错的判断，
要么守住一条安全边界；「把每个分支都补一条」只会让测试文件比被测代码还长，
而且改一次实现要跟着改十处断言。宁可少写，也不要写一堆永远不会红的断言。
新增测试优先并进已有文件，不为一个函数单开一个 `_test.go`。

| 层 | 方式 | 覆盖到什么程度 |
|---|---|---|
| `pkg/crypto`、`pkg/quota`、`pkg/mailer` 纯函数 | 单测，golden case 取自 outlookEmail 常量表 | 判断逻辑复杂或搞错了会静默出错的函数（格式识别、编码解码、配额计算） |
| `pkg/mailer` 协议 | `httptest` + IMAP 桩服务器 | 回退链、错误分类、代理归因这三条主路径 |
| `pkg/repo` **跨引擎对照** | 同一 filter 用例表在 SQLite + PG 上断言结果一致 | 双引擎策略的前提，不可省 |
| `pkg/service` | 集成测试 | 关键路径（事务回滚、级联、配额强制） |
| API | `api/routes_test.go` 扩展 | **每类资源一条越权测试 + 每个 admin 端点一条非管理员 403 测试** |
| 前端 | Vitest | 纯逻辑与安全相关的那几处（`selectionStore` / `messageDocument` / `mailBase`） |

**两类隔离测试是不可省的**：

1. 租户隔离——A 租户用户访问 B 租户资源 → 404/403
2. 平台角色隔离——普通用户访问 `/admin/*` → 403

这两类漏洞在多租户 SaaS 里极其常见，后果是直接泄露他人邮箱凭据。

## 2. 风险登记

| # | 风险 | 影响 | 应对 |
|---|---|---|---|
| R1 | `go-imap/v2` 仍是 beta，API 可能变 | P2 返工 | go.mod 精确锁版本；封装在 `imapx` 内不外泄类型；预留退回 v1 的方案 |
| R2 | **两套 SQL 语义漂移**（只改了一边） | 换库后行为不一致，极难排查 | `parity_test.go` 跨引擎对照测试 + CI 跑 PG service container；这是双引擎策略成立的前提 |
| R3 | 微软随时调整 OAuth/Graph 行为 | 全平台失效 | 错误分类 + 通道回退已是缓解；`cmd/mailprobe` 快速定位；scope 与端点集中在常量文件便于热修 |
| R4 | SQLite 单连接在批量写入下成为瓶颈 | 导入/刷新极慢 | 部署文档明确「生产用 PostgreSQL」；导入分批事务；SQLite 开 WAL |
| R5 | 内存限流器不支持多实例 | 多实例部署限流失效 | **本方案明确单实例部署**；要上多实例再换实现 |
| R6 | **Kumo 缺 Tree / 虚拟化 / ContextMenu** | 三个核心交互要自建 | 已在 [06 文档 §2.1](06-frontend.md) 列出自建清单与依赖；自建组件遵循 Kumo 的 forwardRef/displayName/`cn()` 约定 |
| R7 | Kumo 的令牌约束与模板既有样式冲突 | 亮/暗模式错乱 | P0 一次性清理 `style.css` 与 5 个既有页面；ESLint 规则长期拦截 |
| R8 | 前端类型与后端 model 手工同步易漂移 | 运行时错误 | 集中在 `types/mail.ts`；类型量级不大，手写即可，暂不引入代码生成 |
| R9 | 导出接口泄露全量凭据 | 灾难级 | 独立权限 `account:secret` + 二次密码验证 + 强制审计 + 频率限制 |
| R10 | 邮件 HTML 的 XSS | 多租户下爆炸半径是整个平台 | DOMPurify + sandbox iframe 双层（[06 文档 §6.1](06-frontend.md)） |
| R11 | 批量刷新触发微软风控导致账号被封 | 用户资产损失 | 账号间延迟可配、并发数可配、检测到 abuse mode 立即停该账号并标记；配额天然限速 |
| R12 | 管理员权限滥用 | 信任崩塌 | 跨租户读写全审计、不做 impersonation、管理员名单后台可见（[08 文档 §2.4](08-saas-admin.md)） |
| R13 | 注册开放导致薅配额 | 成本失控 | 默认套餐给保守额度；`REGISTRATION_MODE` 可随时切 `invite`/`closed`；IP 限流 |
| R14 | 依赖从 6 个涨到 10+ | 供应链面扩大 | 依赖限制在 `mailer` 包内；`.github/dependabot.yml` 保持开启 |

## 3. 合规提醒

本平台是**面向公众开放注册的 SaaS，且托管的是第三方邮箱凭据**，敏感度高于自用工具：

- 服务条款须明确「用户保证对其托管的邮箱账号拥有合法授权」，
  注册页与导入页各展示一次提示
- 生产必须 HTTPS（`COOKIE_SECURE=true`）+ `ENCRYPTION_KEY` 强随机 + 定期轮换
- 导出、管理员跨租户访问——两条数据出口全部走审计
- 遵守微软及各邮件服务商的服务条款；批量刷新频率设置应保守
- 用户注销时必须物理清除凭据密文，不能只软删

## 4. 建议的第一步

P0 的第 5 项（**注册五件套事务化**）优先做——它决定了此后所有功能的数据形态，
且一旦上线后再改就需要给存量用户补数据。
其次是 `pkg/crypto`（P1 的账号表落地依赖它）与 Kumo 接入（前端所有页面的前提）。

原先列为「未知项」的 sqlc 动态查询问题已定论：**两个引擎各写各的 SQL**，
细节见 [03 文档 §5.1](03-data-model.md)，不再需要 spike。

## 5. 已移出范围（2026-08-21）

下列能力曾作为 P5–P7 规划，现已**从方案中删除**，各文档中的对应设计（表结构、接口、
页面、依赖）一并移除。不是「延后」，是不做；日后若要重新纳入，按新需求重写设计，
不要去翻历史版本：

| 已删除 | 曾经的位置 |
|---|---|
| 邮件转发（SMTP / Telegram / 企业微信）与转发去重历史 | P5 |
| cron 调度器（定时刷新、转发轮询、日志归档） | P5 |
| 租户键值设置表 `tenant_settings` 与 `/mail/settings` 接口 | P5 |
| API Key 与对外 API（`/api/external/v1`） | P6 |
| 邮件分享链接与公开只读页 | P6 |
| 本地邮件保留（`mail_retained_messages`） | P6 |
| 临时邮箱、项目账号领取、邀请码/邮箱验证、WebDAV 备份、多实例、计费、浏览器扩展 | P7 |

**遗留物已于 2026-08-25 清理完毕**（`000006_drop_unused` + 同批代码改动）。
当初为转发 / 对外 API 预埋的字段、权限常量、路由与配置项全部删除，清单见
[PROGRESS.md](PROGRESS.md)。删除的动机不只是省几列：`plans` 的三个 `allow_*` 开关
在后台是可点的，用量页还把它们当作套餐权益展示给用户——一个点了之后什么都不会发生的
开关，比没有这个开关更糟。

**日后若要重做这些能力**，按新需求重写设计，不要去翻历史版本，也不要指望库里还留着位置。
