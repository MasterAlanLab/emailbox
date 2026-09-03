# 05 · API 设计

## 1. 约定

- 前缀：`/api/v1`
- 响应体一律 `{code, data, message}`（AGENTS.md §5.1）；分页用 `{items, pagination}`
- 方法：只用 `GET / POST / PATCH / DELETE`（**不用 PUT**——`main.go` 的 CORS
  `AllowMethods` 目前不含 PUT，且与模板现有风格一致）
- 租户作用域：挂在模板已有的 `t := protected.Group("/tenants/:tenantID", tenant.Member)` 下，
  权限用 `middleware.Require(model.PermissionXxx)` 声明
- 批量操作统一用 `POST .../batch/<action>`，请求体带 `account_ids: []string`，
  返回 `{requested, succeeded, failed, errors: [...]}`

### 1.1 两套路由组

| 路由组 | 前缀 | 鉴权 | 租户来源 |
|---|---|---|---|
| 用户 | `/api/v1/tenants/:tenantID/...` | 会话 Cookie + `tenant.Member` | URL，且必须是自己所属租户 |
| 管理员 | `/api/v1/admin/...` | 会话 Cookie + `RequirePlatformAdmin` | URL 显式指定（`/admin/tenants/:tenantID/mail/...`） |

用户这一组还接受第二种身份：`Authorization: Bearer <API Key>`。Key 不是第二套接口，
而是一个只读的虚拟角色 `model.TenantRoleAPI`——鉴权后走同一份路由，权限由
`middleware.Require` 收敛到 `group:read` / `account:read` / `message:read` 三项。
详见 §3.1。

**两者共用同一份 service 与 repo**。管理员路由组只是「谁有权决定 tenantID」不同，
SQL 里的 `WHERE tenant_id = ?` 永不放宽——理由与实现见 [08 文档 §2.3](08-saas-admin.md)。

### 1.2 业务错误码

`code` 字段除 0（成功）/ 1（通用失败）外，为需要前端分支处理的场景约定专用码：

| code | HTTP | 含义 |
|---|---|---|
| `1001` | 403 | `QUOTA_EXCEEDED` — 超出配额，`data` 为 null，上限与已用量写在 `message` 里 |
| `1003` | 403 | `ACCOUNT_DISABLED` — 用户已被管理员禁用 |
| `1004` | 409 | `MAIL_ACCOUNT_EXISTS` — 邮箱已存在 |
| `1005` | 502 | `UPSTREAM_MAIL_ERROR` — 上游邮件服务失败，`data` 带 `{error_kind, channel}` |

> **1005 的状态码细分**（`handler.upstreamFailure`）：`banned` → 409、`rate_limited` → 429、
> `folder_unavailable` → 404、`canceled` → 504；其余（含 `auth_failed` / `consent_required`）
> 落到表里的 502。
>
> **其中绝不能出现 401。**401 的含义是「本次请求的调用方没通过认证」，
> 而托管邮箱凭据失效说的是「调用方没问题，是他托管的那个邮箱过期了」。
> 实现一度把 `auth_failed` 映射成 401，后果是用户导入的一批账号里只要有一个 token 失效，
> 点开它就把**用户本人**踢回登录页。
> 要区分处置方式请读 `data.error_kind`，不要靠状态码。
> 回归用例：`api/mail_messages_test.go:TestUpstreamAuthFailureIsNotUnauthorized`。

## 2. 需要先改的模板既有装配

### 2.1 请求体大小限制

`main.go` 现在是全局 `middleware.BodyLimit(64 * 1024)`。导入接口会超。

> **实施时的修正**：原方案设想「路由级 BodyLimit 覆盖全局的那个」，
> 但 Echo v5 并非如此——**两层都会生效，且更严的那个说了算**
> （请求体先被全局中间件包成 `limitedReader`，路由级再包一层也解不开外面那层）。
> 只挂路由级中间件的话，导入接口仍然在 64KB 处返回 413。

实际改法是「全局 Skipper 放行 + 路由级重新限制」这一对：

```go
// api/routes.go
func GlobalBodyLimit() echo.MiddlewareFunc {
    return echomw.BodyLimitWithConfig(echomw.BodyLimitConfig{
        LimitBytes: DefaultBodyLimit, // 64KB
        Skipper:    func(c *echo.Context) bool { return IsBulkPath(c.Request().URL.Path) },
    })
}
func BulkBody() echo.MiddlewareFunc { return echomw.BodyLimit(BulkBodyLimit) } // 8MB

// main.go
e.Use(api.GlobalBodyLimit())

// 路由
m.POST("/accounts/import", h.Account.Import, middleware.Require(...), api.BulkBody())
m.POST("/accounts/batch/delete", h.Account.BatchDelete, ..., api.BulkBody())
```

`IsBulkPath` 按 §1 的命名约定识别（`/import` / `/export` 结尾、`/batch/` 中缀），
导出也在其列：按 ID 选中 5000 个账号时，光 UUID 列表就有 ~180KB。
新增大请求体接口只要遵守命名就自动生效。两个中间件必须成对出现——
少任何一半的表现都是导入接口对稍大的文件返回 413，而 413 完全不会让人
联想到中间件配置。`api/bodylimit_test.go` 固定了这个组合行为。

Echo 的后注册中间件会覆盖前面的同类中间件（BodyLimit 读的是最近一个设置），
实施时需要用一个测试固定这个行为，避免升级 Echo 后静默失效。

### 2.2 SSE 与 Gzip / WriteTimeout

`main.go` 里 `e.Use(middleware.Gzip())` 会缓冲 SSE 输出，`WriteTimeout = 30s` 会掐断长连接。
任务流式接口必须：

- Gzip 中间件配置 `Skipper`，跳过 `/jobs/*/stream`
- SSE handler 内 `c.Response().Unwrap()` 拿到底层 `http.ResponseWriter`，
  或用 `http.ResponseController.SetWriteDeadline(time.Time{})` 取消该连接的写超时
- 每 15s 发一个 `: keepalive` 注释帧防止中间代理断流

这两点不解决，SSE 会表现为「进度条卡住 30 秒后断开」，很难排查。

## 3. 分组

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/mail/groups` | group:read | 返回分组列表（各带账号数），顺序即 sort_order；代理只给 `*_masked` |
| GET | `/mail/groups/:groupID/proxy` | **account:secret** | 代理**明文**，供编辑表单回填；强制审计 |
| POST | `/mail/groups` | group:write | 创建 |
| PATCH | `/mail/groups/:groupID` | group:write | 改名/颜色/描述/代理/定时刷新间隔 |
| DELETE | `/mail/groups/:groupID` | group:write | 删除，账号回落默认分组 |
| POST | `/mail/groups/reorder` | group:write | 排序，body `{group_ids: []}` |

分组是平的一层（2026-08-27 起，见 PROGRESS「分组压平成一层」）：没有 `parent_id`、
没有 `level`，也没有「含子树的账号数」。

**代理明文只从 `/proxy` 这一个端点出去。**它是读操作，但收敛口径跟着「读走了什么」
而不是「谁在读」走，因此按导出同一档：`account:secret` 权限（API Key 天然没有这一项）
＋ `AuditWrite` 而不是 `AuditAdminRead`——普通用户取走一条代理口令同样要留痕。
没配限流：一次只出一个分组的一条代理（`000016` 去掉两个备用位之后就只剩一条了），
而分组数本身受配额约束，与「一次取走整租户凭据」的导出不是一个量级。

它存在的理由是编辑表单（06 文档 §6.2「代理 URL 在输入框以外一律显示打码版」）：
回填打码串的话，用户改完名字一按保存，`socks5://u:****@host` 就被当成口令原样存回库里，
代理从此是坏的，而界面上看不出来。配套地，`PATCH` 的代理三项是指针语义——
**不传即保持原值**，只有显式传空串才是清空；前端在明文没读回来时正是靠这一条
整组省掉代理字段。两条都由 `api/mail_groups_test.go:TestGroupProxyRoundTrip` 钉住。

**定时刷新的间隔也挂在分组上**（`refresh_interval_minutes`，2026-09-03 起）。
`GET /mail/groups` 一并回 `refresh_interval_minutes` 与 `next_refresh_at`，
`PATCH` 收前者，取值是 0（关闭）或 10080~43200 分钟（7~30 天）。语义与执行见 §6.2。

不为它单开 `/mail/groups/:groupID/schedule`：它就是分组的一个属性，多一个端点等于
多一处租户校验、权限与审计要维护。这个字段同样是指针语义，但和代理相反——
**0 是有含义的取值（关闭），不是「没传」**，因此后端收 `*int`，
前端也不能用 falsy 判断把它过滤掉。

### 3.1 API Key（对外取件）

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/tenants/:tenantID/api-key` | tenant:update | 回显明文 Key，未生成时 `data: null` |
| POST | `/tenants/:tenantID/api-key/reset` | tenant:update | 生成并覆盖，旧 Key 当场失效，写审计 `api_key.reset` |
| GET | `/llms.txt` | 公开 | 给 Agent 的接入说明，由 `handler.APIEndpoints` 渲染，不含任何 Key |

一个租户一把 Key，存两列：`token_hash` 供鉴权按 O(1) 命中，`token_enc` 是 AES-GCM
密文、只为页面回显（迁移 `000012` 的注释里写了这个取舍的代价）。

Key 能调的就是 `handler.APIEndpoints` 里那五条只读端点，且只能是自己所属的租户。
它拿不到 `mail:account:secret`（导出）与 `tenant:update`——**读不到也重置不了自己**，
泄露后无法自我续命。`api/apikey_test.go` 把这份矩阵钉死了。

## 4. 账号

### 4.1 CRUD 与查询

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/mail/accounts` | account:read | 见下方查询参数 |
| GET | `/mail/accounts/:accountID` | account:read | 详情（凭据字段一律脱敏） |
| POST | `/mail/accounts` | account:write | 单个新增 |
| PATCH | `/mail/accounts/:accountID` | account:write | 改分组/备注/状态/代理/别名/凭据 |
| DELETE | `/mail/accounts/:accountID` | account:delete | 软删除 |

`GET /mail/accounts` 查询参数：

```
group_id=<id>                         分组
q=<关键词>                            匹配 email / remark（多词 AND）
status=active|disabled|banned
refresh_status=never|success|failed
provider=outlook|gmail|...
sort=created_at|email|sort_order|last_refresh_at   order=asc|desc
page=1&limit=50                       limit 上限 200
```

响应中账号对象的凭据字段处理：

```json
{
  "id": "...", "email": "user@outlook.com", "provider": "outlook",
  "has_password": true, "has_refresh_token": true, "client_id": "24d9a0ed-...",
  "proxy_url_masked": "socks5h://outlook.{mail}:****@127.0.0.1:2260",
  "aliases": ["a@x.com"], "tags": [{"id":"..","name":"..","color":".."}],
  "last_refresh_status": "failed",
  "last_refresh_error": "[auth_failed] refresh_token 因长期未使用而过期，请重新授权（AADSTS700082）",
  "auth_channel": "graph"
}
```

**明文凭据永不出现在列表/详情接口**，只在导出接口（需 `account:secret`）中返回。
`client_id` 不算敏感（它是应用标识，不是凭据），保留明文便于排障。

### 4.2 批量导入

```
POST /mail/accounts/import          权限 account:write   BodyLimit 8MB
{
  "group_id": "...",
  "format": "outlook_oauth | imap | custom_imap",
  "content": "user@outlook.com----pwd----uuid----token\n...",
  "on_conflict": "skip | update",
  "defaults": { "remark": "", "tag_ids": [], "status": "active" },
  "imap_host": "", "imap_port": 993          // format=custom_imap 时可选统一指定
}
→ { "total":5000, "created":4812, "updated":100, "skipped":60, "failed":28,
    "errors":[{"line":137,"email":"x@y.com","reason":"..."}], "truncated": false }
```

解析规则见 [04 文档 §7](04-mail-protocol.md)。
**实现要点**：整批放一个事务里会长时间持锁（SQLite 会直接卡死其他请求），
应按 500 行一批分事务提交，失败的批次不影响已提交批次，并在响应里如实反映。

### 4.3 批量操作

| 路径 | 权限 | body |
|---|---|---|
| `POST /mail/accounts/batch/move` | account:write | `{account_ids, group_id}` |
| `POST /mail/accounts/batch/proxy` | account:write | `{account_ids, proxy_url, fallback_1, fallback_2}` |
| `POST /mail/accounts/batch/status` | account:write | `{account_ids, status}` |
| `POST /mail/accounts/batch/delete` | account:delete | `{account_ids}` |

统一返回：`{requested, succeeded, failed, errors:[{account_id, reason}]}`。
`account_ids` 长度上限 5000，超出返回 400（提示分批）。

### 4.4 导出

```
POST /mail/accounts/export         权限 account:secret
{ "scope": "group|selected|all", "group_ids": [], "account_ids": [] }
→ text/plain 附件下载，格式与 outlookEmail 一致（可被本平台重新导入）
  响应头 X-Export-Count 带回实际导出的条数
```

这是全平台风险最高的接口，三道闸门：独立权限 `account:secret`、强制写 `audit_logs`、
按用户限流。

> **二次密码验证已于 2026-08-27 去掉。** 原设计照搬 outlookEmail 的
> `export_verify_tokens`，让操作者在导出前再输一遍自己的登录密码。取消它是产品决定：
> 这个平台的用户就是来批量取自己的凭据的，导出是常规操作而不是危险动作，
> 每次都拦一道密码框换来的主要是「把密码练成肌肉记忆」。
> 剩下三道闸门没有松，其中审计与限流才是事后能追责、事中能限速的那两件。

实施细节：

- `scope=group` 只导这些分组下的直属账号（分组已经是平的一层，没有子树可展开）
- 限流按**用户**而不是 IP（10 次/分钟）：同一出口 IP 后面可能有几十个用户，
  按 IP 限会互相误伤；而攻击者拿到会话后换 IP 是零成本的，按 IP 也拦不住
- 单次上限 `MaxExportAccounts = 20000`，超出提示按分组分批
- 没有可用凭据的账号直接跳过：导出来也无法再导入，不如不占一行
- 凭据里含分隔符 `----` 的账号导出会报错而不是照写：写出去的那一行重新导入时
  会被切成完全不同的字段，属于静默的数据损坏

## 5. 邮件

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| GET | `/mail/accounts/:accountID/messages` | message:read | `folder=inbox\|junkemail\|deleteditems\|all`，`skip`、`top`（≤50）、`refresh=true` 强制走远端 |
| GET | `/mail/accounts/:accountID/messages/:messageID` | message:read | `folder`、`id_mode` 必须透传 |
| GET | `.../messages/:messageID/attachments/:attachmentID` | message:read | 二进制流下载 |
| GET | `.../messages/:messageID/attachments.zip` | message:read | 全部附件打包 |
| POST | `.../messages/read` | message:write | `{items:[{id, folder, id_mode}]}` 批量标已读 |
| POST | `.../messages/delete` | message:write | 同上，批量永久删除 |

响应里必须回传 `channel`（本次实际走通的通道），前端据此显示实际走通的链路。

> 响应里还有一个 `source` 字段，恒为 `remote`。它是给已删除的「本地邮件保留」
> 预留的位置（那些能力已移出范围，见 [README](README.md)），现在没有第二个取值。

**筛选参数**：
`subject_contains`、`from_contains`、`keyword`（在正文里搜，会触发详情补齐，代价高，需限流）。

## 6. Token 刷新与任务

单个与批量刷新共用收信的 OAuth 通道顺序：优先上次成功通道，Graph 或 IMAP 的 OAuth
令牌端点认证或权限失败时继续尝试其它 OAuth 通道。任一通道令牌交换成功，
账号最近状态、任务明细与刷新日志均记成功；响应中非空且与当前值不同的
refresh token 加密落库。刷新只请求令牌端点，不取件、不消费每日取件额度。
失败仍使用现有 `error_kind`，但 `message` 区分过期、撤销、权限、客户端配置等原因，
未确认原因的 `invalid_grant` 不再显示为已过期。封禁、取消等停止边界保持不变。

| 方法 | 路径 | 权限 | 说明 |
|---|---|---|---|
| POST | `/mail/accounts/:accountID/token/refresh` | token:refresh | 单个同步刷新，直接返回结果 |
| POST | `/mail/jobs/token-refresh` | token:refresh | 批量，body `{scope:"all\|failed\|selected\|group", account_ids:[], group_ids:[]}` → 返回 `{job_id}`（`selected` 取 `account_ids`，`group` 取 `group_ids`） |
| GET | `/mail/jobs` | account:read | 任务列表，`type`、`status` 筛选 |
| GET | `/mail/jobs/:jobID` | account:read | 任务详情 + 计数 |
| GET | `/mail/jobs/:jobID/items` | account:read | 逐账号结果，`status=failed` 筛选 |
| GET | `/mail/jobs/:jobID/stream` | account:read | **SSE**，支持 `Last-Event-ID` 断线续看 |
| POST | `/mail/jobs/:jobID/stop` | token:refresh | 请求停止 |

SSE 事件格式：

```
id: 42
event: progress
data: {"total":5000,"success":3120,"failed":88,"current":"u***@outlook.com"}

id: 43
event: item
data: {"account_id":"...","email":"u***@outlook.com","status":"failed","error_kind":"banned"}

id: 44
event: finished
data: {"status":"partial","success":4900,"failed":100,"error_summary":"..."}
```

### 6.1 刷新统计

```
GET /mail/refresh/stats        → {total, success, failed, never, last_job:{...}, by_error_kind:{banned:47,...}}
GET /mail/refresh/logs         → 分页，支持 status / account_id / 时间范围
```

### 6.2 定时刷新（2026-09-03）

没有新端点。间隔配置走 `PATCH /mail/groups/:groupID`（见 §3），执行由进程内的
`service.RefreshScheduler` 每分钟扫一次 `mail_groups.next_refresh_at` 完成，
到期就用 `scope=group` 提交一个和手点完全一样的刷新任务——只是
`jobs.trigger` 记 `scheduled`、`created_by` 为空，刷新日志记 `refresh_type=scheduled`。
因此进度、SSE、停止、统计全部原样复用，前端不需要第二套东西。

存间隔而不是 cron 表达式：用户关心的是「多久碰一次令牌」，而 cron 的「每天 3 点」
离开租户时区就没有意义，`tenants` 表里没有那个概念。

三条推进规则（`pkg/service/refresh_scheduler_test.go` 逐条钉住）：

| 情况 | 提交任务 | 推进 `next_refresh_at` |
|---|---|---|
| 到期，租户没有 token_refresh 在跑 | 是 | `now + interval` |
| 到期，租户已有同类任务未终结 | 否 | **不推进**，下一轮再看 |
| 到期，分组下没有可刷新账号 | 否 | `now + interval` |

两条容易写错的地方：

- **基准是 `now`，不是旧的 `next_refresh_at`。**按旧值累加的话，停机三天再启动时
  一个 6 小时间隔的分组会被判定为欠了十二个周期然后连着补跑——那不是补进度，
  是把服务商直接打到风控。
- **租户忙时不推进周期。**推进的话，用户给五个分组设同样的间隔时，只有排在最前面
  那个会真的被刷：其余每轮都恰好撞上「忙」再被推到下个周期，
  表现为「有几个分组从来没被定时刷过」。不推进则它们只是排队。

**被管理员禁用的用户，其分组不再被扫到**（扫描 SQL 里的 `EXISTS` 子句）。
这道检查没有别的地方可放：其它刷新入口都走鉴权，禁用用户在那里已经被 `1003` 挡住了，
而调度器是这个系统里第一个「没有登录用户也会动」的东西。少了它，平台会替一个谁都
登不进去的账号，每个周期继续打一轮服务商。周期也不推进，用户被重新启用后立刻就是到期状态。

同一租户不叠加刷新任务（`repo.CountActiveJobsByType`）：`job.Manager` 每个任务各起
`JOB_WORKERS` 个 worker，两个任务并行就是双倍并发打同一个服务商，而
`JOB_ACCOUNT_DELAY_MS` 只在单个 worker 内部起作用，拦不住这种叠加。跨租户不额外限制
——两个用户同时点「刷新全部」这个情况本来就存在，不是定时引入的。

**保留期清理是这个功能的前置条件，不是附赠。**手动刷新是低频的，`jobs` / `job_items` /
`mail_refresh_logs` 一直没有清理也没出过问题；定时刷新把它们变成「账号数 × 每天轮次」
的稳定增量（5000 账号每天四轮 ≈ 每天两万行）。`main.go` 的 `purgeJobs` 与
`purgeRefreshLogs` 各保留 30 天，写成常量。

## 7. 已删除的端点组

- **`/mail/tags` 与 `POST /mail/accounts/batch/tags`**（2026-08-27，`000007_drop_tags`）。
  标签能建能删，账号响应里也带 `tags`，但**没有任何端点被前端用来把标签贴到账号上**，
  于是那个数组永远是空的。给账号分类这件事由分组承担——它有配额、有完整的管理页。
  两套平行机制里只留能用的那套。
- **`/mail/settings`（租户级键值设置）**。它承载的全部是转发与本地保留的配置项，
  随那两个功能一起删掉（见 [README「明确不做的事」](README.md)）；`tenant_settings` 表也不再需要。

## 8. Microsoft OAuth 重新授权

| 方法 | 路径 | 说明 |
|---|---|---|
| POST | `/mail/accounts/:accountID/oauth/start` | 创建一次性授权流程，返回微软授权链接 |
| GET | `/oauth/microsoft/callback` | 微软公开回调；校验 state、换取令牌后只保存密文 |
| POST | `/mail/accounts/:accountID/oauth/complete` | 完成授权；也可提交 `{flow_id, redirected_url}` 兼容本地回调地址 |

流程使用授权码 + PKCE。`state` 随机且一次性，流程 10 分钟过期；授权码、访问令牌与
refresh token 都不会出现在业务接口响应里。换到令牌后先调用 Microsoft Graph `/me`
核对邮箱身份，再验证 refresh token；全部成功后才以窄 UPDATE 替换账号的
`client_id` / `refresh_token` / `auth_channel` 与最近刷新状态，失败时保留旧凭据。

默认先沿用参考项目的 Microsoft 应用及其 `http://localhost:8080` 回调地址。因此前端同时
提供“粘贴最终跳转地址”入口：浏览器即使打不开本地回调，地址栏仍含有授权结果，服务端会
校验同一份 state 后完成交换。部署方注册自己的 HTTPS 回调后，配置
`MICROSOFT_OAUTH_REDIRECT_URI` 指向 `/api/v1/oauth/microsoft/callback`，即可自动返回 Token 页。

重新授权只适用于 Outlook OAuth 账号，并要求 `account:write`。流程记录始终带
`tenant_id`、账号和发起用户，跨租户账号 ID 与流程 ID 均返回 404；公开回调也通过 state
内的租户和流程标识执行带 `tenant_id` 的查询。审计动作记为 `token.reauthorize`。

## 9. 限流

| 范围 | 策略 |
|---|---|
| `/auth/register`、`/auth/login` | 模板已有：0.2 次/秒，burst 10，按 IP |
| 批量导入 / 导出 | 按用户，10 次/分钟 |
| 邮件拉取（`refresh=true`） | 按账号，6 次/分钟（保护上游，避免被微软风控） |
| `keyword=` 正文搜索 | 按用户，10 次/分钟 |

模板已用 `echomw.RateLimiterWithConfig` + 自定义 `DenyHandler` 返回统一响应格式，
新增限流器沿用同一写法。注意内存限流器在多实例下不共享；
若上线多实例，需要换成基于数据库或 Redis 的实现——不过多实例本身也在
「明确不做的事」里（见 [README](README.md)）。

限流与配额是**两回事**：限流防瞬时滥用（按秒/分钟），配额防长期超用（按天/总量）。
两者都要有，配额见 §11。

## 10. 管理员 API

全部挂在 `/api/v1/admin`，中间件 `RequirePlatformAdmin`。

### 10.1 用户管理

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/users` | 分页 + 搜索（用户名/邮箱）+ 筛选（status、platform_role）。返回含 `tenant_id`、`account_count`、`quota_usage`、`last_login_at` |
| GET | `/admin/users/:userID` | 详情 |
| PATCH | `/admin/users/:userID` | 改 `status` / `platform_role`。**禁用时同步清空该用户全部 sessions**；不能撤销最后一个管理员 |
| POST | `/admin/users/:userID/reset-password` | 生成临时密码，一次性返回；清空该用户全部 sessions |
| DELETE | `/admin/users/:userID` | 软删用户 + 其个人租户下的邮箱账号，并**物理清除凭据密文列** |
| ~~POST~~ | ~~`/admin/users`~~ | **未实现**。关闭注册时的建号由 `BOOTSTRAP_ADMIN_USERNAME` / `_PASSWORD` 在启动时完成，后台里没有「新建用户」入口 |

### 10.2 跨租户邮箱管理

```
/admin/tenants/:tenantID/mail/**        *     与 /api/v1/tenants/:tenantID/mail/** 完全同构
```

**没有 `GET /admin/tenants`（租户列表）**：一个租户空间只属于一个用户，那份清单的每一行
都能在 `/admin/users` 里找到对应的用户，两份列表只会让人怀疑「这两个数为什么对不上」。
账号数、套餐、配额上限与超额标记因此都并进了用户行。该端点连同 `AdminTenantsPage`
一起删除（前者 2026-08-27，后者更早）。

上面这行是关键：**路径结构、请求体、响应体与用户侧一模一样**，
handler 复用同一份实现，只是 tenantID 的来源与鉴权中间件不同。
前端因此可以直接复用 `/mail` 的全部组件（[06 文档 §7](06-frontend.md)）。

管理员访问他人租户时，前端常驻显示「你正在以管理员身份操作」的 Banner，
由路由决定（`AdminTenantMailPage`）——跨租户邮箱只能从那个页面进。

原方案在这里加过一个 `X-Admin-Context: <tenantID>` 响应头供前端识别，已删除：
前端从未读取，且管理员身份本就在会话的 `platform_role` 里，无须逐个响应重复声明。

### 10.3 套餐与配额

| 方法 | 路径 | 说明 |
|---|---|---|
| GET / POST | `/admin/plans` | 套餐列表 / 新建 |
| PATCH / DELETE | `/admin/plans/:planID` | 修改 / 删除（有租户在用时拒绝删除） |
| GET | `/admin/tenants/:tenantID/quota` | 该租户的生效配额 + 当前用量 |
| PATCH | `/admin/tenants/:tenantID/quota` | 切换套餐或设置单项覆盖值，必须带 `note` 说明原因 |

### 10.4 平台运维

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/admin/audit` | 全平台审计日志，按 tenant / actor / action / 时间筛选 |
| GET | `/admin/stats` | 平台概览：用户数、租户数、邮箱总数、今日拉信量、失败率 |

曾经列在这里的 `GET /admin/jobs`（全平台任务列表）**没有实现**：任务是按租户跑的，
管理员要看某个用户的任务，从 `/admin/tenants/:tenantID/mail/jobs` 进去即可，
再多一个跨租户的合并视图没有对应的使用场景。

### 10.5 审计要求

管理员的**读操作也要审计**（普通用户的读不记，量太大）。必须记录的三类：

- 查看某租户的账号列表
- 查看某封邮件正文
- 导出账号

写操作照常全部审计，`actor_kind='admin'`。

## 11. 配额的接口约定

- 每个受配额约束的接口在超额时返回 `code=1001` + HTTP 403。`data` 为 null，
  上限与已用量写在 `message` 里（前端直接展示这句话，不做二次拼装）：
  ```json
  { "code": 1001, "data": null,
    "message": "QUOTA_EXCEEDED: 邮箱账号 数量上限为 50，当前 50" }
  ```
- **批量导入是例外**：超额部分计入 `skipped`，已导入的保留，HTTP 200：
  ```json
  { "code": 0, "data": { "total": 200, "created": 30, "skipped": 170,
                         "skipped_reason": { "quota": 170 } },
    "message": "部分导入成功" }
  ```
- 用户侧查询自己的配额：`GET /api/v1/tenants/:tenantID/quota`
  → `{ plan: {...}, limits: {...}, usage: {...} }`，供设置页的 `Meter` 展示
