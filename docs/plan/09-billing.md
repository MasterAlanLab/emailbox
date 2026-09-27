# Waffo 订阅与计费设计

**状态：代码与测试已完成（2026-09-27），待在 Waffo 测试环境做真实支付验收（§10 第 5 步）**

本文定义 Emailbox 通过 Waffo Pancake 提供订阅支付、套餐价格管理与额度权益绑定的实现边界。
现有配额计算仍由 `pkg/quota` 负责；Waffo 只负责收款、订阅生命周期与支付事件通知。

## 1. 目标与边界

### 1.1 目标

- 平台管理员可以在后台打开或关闭支付入口。
- 管理员可以为现有套餐设置月付 / 年付价格、币种和额度，并查看 Waffo 产品同步状态。
- 注册用户可以查看套餐、发起订阅、查看当前订阅状态，并在 Waffo 侧完成付款。
- 付款成功后，订阅事件驱动租户套餐和额度；浏览器跳转页只展示状态，不承担授权。
- 保留免费默认套餐。支付入口关闭时，免费套餐和已有订阅的 Webhook 处理继续工作。

### 1.2 这次明确采用的方案

1. **Go 直接调用 Waffo HTTP API**。服务端自行完成请求签名、错误映射和 Webhook 验签，项目中不引入 Bun Bridge、Node 运行时或 `@waffo/pancake-ts` 作为服务端依赖。
2. Waffo 私钥只从运行时密钥注入，进入 `pkg/waffo` 后留在内存中；配置、前端、数据库、审计字段、日志和 Git 都只出现密钥标识或摘要。
3. `tenant_quotas` 继续作为配额计算的唯一入口。订阅变更通过受控 service 更新套餐来源，管理员直接分配的套餐优先保留。
4. Waffo Webhook 是订阅权益的权威来源。支付回跳、前端提示和客户端传入的金额都不直接授予权益。
5. 测试环境与生产环境分别使用 Merchant / Store / API Key / Webhook 公钥，数据库记录 `mode`，事件跨环境时直接隔离。

官方文档明确支持 REST 写入、GraphQL 读取以及任意语言自行签名；因此 Go HTTP 客户端是本项目的主路径：

- [Waffo SDK 与直接 API](https://docs.waffo.ai/integrate/sdks)
- [Waffo Webhooks](https://docs.waffo.ai/api-reference/webhooks)
- [Waffo API Reference](https://docs.waffo.ai/api-reference)

## 2. 总体架构

```text
浏览器
  │ 读取套餐 / 请求 checkout URL
  ▼
Echo billing handler ──> BillingService ──> pkg/waffo HTTP client ──> Waffo API
       │                       │                         │
       │                       └── plans / prices / subscriptions / quota
       │
       └── GET /api/v1/webhooks/waffo  <── Waffo HTTP webhook
                 │ 原始 body + RSA-SHA256 验签
                 ▼
          billing_webhook_events（去重）
                 │ 事务处理
                 ▼
       tenant_subscriptions + tenant_quotas
```

建议的代码分层：

| 目录 | 职责 |
|---|---|
| `pkg/waffo` | API 请求签名、HTTP transport、请求 / 响应 DTO、Webhook 验签；不依赖 Echo 与 repo |
| `pkg/service/billing_service.go` | 支付开关、套餐价格、checkout、取消、订阅状态与配额编排 |
| `pkg/handler/billing.go` | 参数解析、统一响应、权限与审计挂载 |
| `pkg/repo/billing.go` | 计费设置、价格、checkout 意图、订阅、Webhook 事件的双引擎查询 |
| `web/src/pages/settings/UsagePage.tsx` | 用户套餐 / 用量 / 订阅状态 |
| `web/src/pages/admin/AdminPlansPage.tsx` | 套餐额度、价格、开关、同步状态 |

Waffo 具体字段集中在 `pkg/waffo`，业务层只接收项目自己的领域结构，避免供应商字段扩散到 `model` 与前端。

## 3. 配置与密钥

### 3.1 环境变量

```text
WAFFO_MERCHANT_ID=MER_xxx
WAFFO_STORE_ID=STO_xxx
WAFFO_PRIVATE_KEY=BASE64_PRIVATE_KEY   # 一行 Base64，或用引号括起的多行 PEM
WAFFO_ENV=test                         # test | prod，API Key 本身也绑定环境
```

- 三项凭据要么都不配（支付不可用，服务照常启动），要么都配且能解析；配一半或私钥无效时启动失败。
- 支付开关只在后台（`billing_settings.enabled`），环境变量里没有开关。
- Webhook 公钥是平台级的，内置于 `pkg/waffo/platform_keys.go`（取自官方 SDK，与 Dashboard 核对一致），
  `WAFFO_WEBHOOK_{TEST,PROD}_PUBLIC_KEY` 仅用于 Waffo 轮换公钥时覆盖。
- 付款回跳地址按发起结账的请求域名推导，仅 https 时携带；`WAFFO_SUCCESS_URL` 仅用于固定地址。
- Merchant ID、Store ID 与 API Key 仅由服务端使用，浏览器只收到一次性 checkout URL。
- 需求中给出的测试凭据只用于本地 / 测试环境配置示例，文档和代码均使用占位符。若该私钥曾用于非临时测试，应在 Waffo 控制台轮换。

### 3.2 请求签名

API Key 请求使用以下 canonical string：

```text
METHOD + "\n" + PATH + "\n" + TIMESTAMP + "\n" + SHA256_BASE64(BODY)
```

使用 RSA-SHA256 和私钥签名后 Base64 编码，发送：

```text
X-Merchant-Id: <merchant id>
X-Timestamp: <unix seconds>
X-Signature: <base64 signature>
```

`pkg/waffo` 只使用 Go 标准库 `crypto/rsa`、`crypto/sha256`、`crypto/x509`、`encoding/pem`、`net/http` 完成这条链路。请求 body 先序列化，再用同一份字节计算摘要并发送，避免签名内容与实际 body 漂移。

## 4. 数据模型与迁移

新增 `000020_billing`，SQLite 与 PostgreSQL 各维护一份 SQL，字段和约束保持同一语义；所有查询带 `tenant_id`，Webhook 事件查询除外且必须以 `store_id + mode` 收敛。

### 4.1 `billing_settings`

单例行 `id = 1`：

| 字段 | 说明 |
|---|---|
| `enabled` | 支付入口开关，默认 `0` |
| `mode` | `test` / `prod` |
| `default_currency` | 后台默认币种 |
| `updated_by` / `updated_at` | 管理员与更新时间 |

Merchant / Store 及私钥仍来自环境变量，避免把供应商凭据放入数据库。

### 4.2 `plan_prices`

套餐与价格拆开保存，`plans` 继续承载额度：

| 字段 | 说明 |
|---|---|
| `id`、`plan_id` | 本地价格变体；关联 `plans` |
| `billing_period` | `monthly` / `yearly` |
| `currency` | ISO 4217 大写码 |
| `amount` | 展示格式字符串，如 `"9.90"`；禁止浮点金额 |
| `provider_product_id` | Waffo 产品 ID，可为空（待同步） |
| `sync_status` | `pending` / `active` / `error` / `inactive` |
| `sync_error` | 脱敏后的最近一次同步错误 |
| `active`、时间字段 | 价格展示与软停用 |

唯一约束：`(plan_id, billing_period, currency)`。当前首期方案支持 USD；Waffo 订阅文档列出的 USD / EUR / GBP / HKD / JPY 可作为后续白名单，CNY 订阅上线前先以测试环境确认渠道能力。

### 4.3 `tenant_subscriptions`

一租户一条当前订阅记录，历史状态由事件表保留：

```text
tenant_id             provider              mode
order_id              provider_product_id   plan_price_id
plan_id               status                currency
amount                current_period_start current_period_end
cancel_at_period_end  last_event_at        created_at / updated_at
```

`status` 对齐 Waffo：`pending`、`active`、`canceling`、`past_due`、`canceled`、`expired`。`order_id` 唯一；`(provider, mode, order_id)` 作为跨环境保护。

### 4.4 `billing_checkout_sessions`

保存一次 checkout 意图：`id`、`tenant_id`、`plan_price_id`、`merchant_external_id`、`provider_session_id`、`idempotency_key`、`status`、`expires_at`、时间字段。`(tenant_id, idempotency_key)` 唯一（不是全局唯一：别的租户用同一个键不该撞上）。同一个键重复提交且会话仍有效时复用会话、重新签发顾客令牌；已失败或过期则返回 409，由前端换新键重来——那个键在 Waffo 侧还缓存着旧会话（24 小时），不能原地重建。`checkout_url` 只存 Waffo 返回的裸地址，**顾客令牌不落库**。

### 4.5 `billing_webhook_events`

保存 `event_id`、`event_type`、`store_id`、`mode`、`payload_sha256`、`status`、`error`、接收与处理时间。`(event_type, event_id, mode)` 唯一——Waffo 的 `eventId` 按事件类型指向不同实体，同一笔付款的 `subscription.activated` 与 `subscription.payment_succeeded` 共用一个 eventId，只按 eventId 去重会把后到的静默丢掉；原始 payload 只在受控排障存储中保留，应用日志记录摘要和 request ID，避免支付地址与邮箱进入普通日志。

### 4.6 `tenant_quotas` 的套餐来源

给 `tenant_quotas` 增加：

```text
plan_source       admin | subscription
subscription_id   nullable FK tenant_subscriptions(id)
```

订阅激活时写入 `plan_source=subscription`；管理员手工换套餐（`UpdateTenantPlan`）时写回 `admin` 并清空 `subscription_id`。取消或到期只回收由该订阅授予的套餐，管理员分配的套餐保持原样。这样可以避免 Webhook 把后台刚调整的额度覆盖掉。

## 5. 关键业务流程

### 5.1 管理员维护套餐与价格

1. 管理员编辑本地套餐名称、账号数、分组数、每日取件额度及月 / 年价格。
2. service 校验配额为 `-1` 或非负整数、金额为合法十进制字符串、币种和周期在白名单内。
3. 本地事务先保存价格变体，再异步调用 Waffo 产品创建 / 更新接口；请求带稳定的幂等键。
4. 成功后回写 `provider_product_id` 与 `sync_status=active`；失败保留本地草稿并显示同步错误，后台可重试。
5. 审计记录价格、额度、开关和同步结果，但只记产品 ID、币种、金额与状态。

### 5.2 打开 / 关闭支付

打开前检查：Merchant / Store、API 私钥、对应环境的 Webhook 公钥、Store 状态、Webhook URL、至少一个 `active` 价格变体。检查通过后事务更新 `billing_settings.enabled=1` 并写 `AuditWrite`。

关闭支付只隐藏新的购买入口、暂停新 checkout；已有订阅的取消、续费、退款和到期 Webhook 继续接收并更新权益。重新打开时复用现有产品 ID，先完成同步检查。

### 5.3 用户发起 checkout

1. 用户请求 `plan_price_id`，service 通过当前 `tenant_id` 查询价格，检查支付开关、模式、价格状态和订阅状态。
2. 创建本地 checkout 意图，生成 `merchant_external_id`，并在事务中登记幂等键。
3. 按官方 SDK 的 authenticated checkout 做法：商户签名调用 `POST /v1/actions/checkout/create-session` 建会话，另调 `POST /v1/actions/auth/issue-session-token`（`productId` + `buyerIdentity=租户 ID`）签发顾客令牌。`buyerIdentity` 会以 `merchantProvidedBuyerIdentity` 出现在该订单之后的每个 Webhook 里。
4. checkout 请求中的 `productId`、`currency`、`buyerEmail`、`successUrl`、`orderMerchantExternalId` 和 `metadata` 全部由服务端根据数据库生成。metadata 至少包含 `tenant_id`、`plan_id`、`plan_price_id`、`checkout_id`。
5. 返回给浏览器的地址是 `{checkoutUrl}?test=true#token=...`（生产环境不带 `test=true`）。token 只在片段里：结账页首屏读取后从地址栏抹掉，不进服务器日志与 Referer。token 不进入日志、数据库、query string 或前端状态。前端**同页跳转**：`await` 之后的 `window.open` 已不算用户手势，会被浏览器拦截。
6. `successUrl` 只在是 `https` 时才发：非 https 的值创建会话时不报错，却会在买家付款那一步被支付渠道拒掉（`[A0003] successRedirectUrl is invalid`）。会话有效期 30 分钟，结账页语言 `zh-Hans`。
7. 成功回跳页只显示“支付处理中 / 已收到结果”，由订阅查询和 Webhook 最终刷新套餐。付款完成前，额度维持原套餐。

### 5.4 Webhook 验证与处理

Waffo HTTP Webhook 使用 `X-Waffo-Signature: t=<timestamp>,v1=<signature>` 与 `X-Waffo-Event`，body 是 JSON envelope，顶层含 `id`、`eventId`、`eventType`、`storeId`、`mode`、`data`。

处理顺序：

1. 路由读取**原始 body**，限制请求体大小；解析 signature 的 `t` / `v1`。
2. 使用 `timestamp + "." + rawBody` 做 RSA-SHA256 验签；Waffo 的 `timestamp` 是毫秒时间戳，按 `mode` 选择 Test / Production 公钥，并校验 45 分钟重试窗口。
3. 校验 `storeId`、事件模式、`eventType`、`eventId`，再以 `event_id + mode` 插入去重表。
4. 已处理事件直接返回 2xx；新事件写入 `pending`，交给事务处理器，HTTP 端在 10 秒内返回。
5. 事务按 `eventType` 更新订阅、当前周期与 `tenant_quotas`；事件乱序时以事件时间和当前状态保护旧事件，重复投递保持幂等。

首期处理这些事件：

| 事件 | 权益动作 |
|---|---|
| `subscription.activated` | 建立订阅，切换到价格对应套餐 |
| `subscription.renewed` / `subscription.recovered` | 更新周期，保持订阅套餐 |
| `subscription.payment_succeeded` | 只登记：它只描述一次扣款，周期滚动由 `renewed` 负责 |
| `subscription.plan_changed` | 立即生效的变更切换套餐 |
| `subscription.plan_change_scheduled` | 只记录预定变更，当前周期继续使用旧套餐 |
| `subscription.plan_change_failed` | 保持旧套餐并记录失败 |
| `subscription.canceling` / `subscription.uncanceled` | 标记或撤销周期末取消，周期内保留权益 |
| `subscription.past_due` | 标记逾期；按配置保留宽限期 |
| `subscription.canceled` | 到期回收订阅套餐，恢复默认 / 管理员套餐 |
| `refund.succeeded` / `refund.failed` | 只登记，**不自动回收权益**。回收只跟着 `subscription.canceled` 走；需要立即停权的退款由管理员在 Waffo 侧取消订阅触发——一笔部分退款就把用户降级，比「多用几天」严重得多 |

无效签名、错误环境、错误 Store ID 返回 401 并按 IP 记日志（请求没有可信身份，挂不进审计表）；数据库暂时故障返回 5xx，让 Waffo 重试；重试也救不回来的事件（找不到租户或价格、租户归属各路来源互相矛盾）记为 `failed` 并回 2xx，让 Waffo 停手。普通日志只记录事件 ID、类型、模式和处理结果。

## 6. HTTP API 设计

### 6.1 管理员

| 方法 | 路径 | 作用 |
|---|---|---|
| `GET` | `/api/v1/admin/billing/settings` | 支付开关、模式、默认币种、配置就绪状态 |
| `PATCH` | `/api/v1/admin/billing/settings` | 打开 / 关闭支付与默认币种 |
| `GET` | `/api/v1/admin/plans` | 现有套餐 + 价格变体 + 同步状态 |
| `POST` | `/api/v1/admin/plans/:id/prices` | 新增月付 / 年付价格 |
| `PATCH` | `/api/v1/admin/plan-prices/:id` | 修改金额、币种、启用状态 |
| `POST` | `/api/v1/admin/plan-prices/:id/sync` | 重试 Waffo 产品同步 |

所有写接口挂 `RequirePlatformAdmin`、`Require(model.Permission...)` 与 `AuditWrite`。API 响应永远不含私钥、签名、buyer token。

### 6.2 租户用户

| 方法 | 路径 | 作用 |
|---|---|---|
| `GET` | `/api/v1/tenants/:tenantID/billing/plans` | 可购买价格与额度 |
| `GET` | `/api/v1/tenants/:tenantID/billing/subscription` | 当前订阅、周期、取消状态 |
| `POST` | `/api/v1/tenants/:tenantID/billing/checkout` | 创建或复用 checkout 意图，返回 URL |
| `POST` | `/api/v1/tenants/:tenantID/billing/sync` | 向 Waffo 核对未确认的结账并返回当前订阅（见 §11 对账） |
| `POST` | `/api/v1/tenants/:tenantID/billing/cancel` | 请求周期末取消 |
| `POST` | `/api/v1/tenants/:tenantID/billing/uncancel` | 撤销周期末取消 |

这些端点沿用现有租户成员中间件与权限矩阵，跨租户 ID 统一返回 404。API Key 只读角色可读取套餐 / 订阅摘要，checkout、取消与恢复需要会话用户的写权限。

### 6.3 Webhook

`POST /api/v1/webhooks/waffo` 是供应商回调入口，采用签名、Store、mode、事件去重和请求限流建立边界。路由需保留原始 body；若 JSON 中间件改变字节序列，验签会失败。

## 7. 前端落点

- `/settings/usage` 改成“套餐与用量”：当前套餐、三项额度、月 / 年切换、价格、订阅状态、周期末取消提示。
- 已有套餐卡继续使用 Kumo `LayerCard`、`Button variant="secondary"`，购买按钮仅在支付开关与价格同步状态满足时出现。
- 购买动作同页跳转到 Waffo 结账页（见 §5.3 第 5 条）；回跳后刷新 `subscription` 与 `quota`，不在 URL 中保存 token 或私密字段。
- `AdminPlansPage` 增加页面级支付开关、模式提示、币种和价格输入、Waffo 产品 ID / 同步状态；私钥字段永远不进入表单。
- 错误以内联红字展示：配置未就绪、价格同步失败、已有活动订阅、Webhook 尚未确认分别给出对应处理动作。

## 8. 幂等、错误与安全

- checkout、产品创建 / 更新、取消 / 恢复均带稳定的本地幂等键；重试只针对安全的网络错误与 5xx。
- 金额使用十进制字符串，服务端校验精度、币种、周期和 Waffo 返回金额；金额以 provider 回调为最终记录。
- Webhook 事件先去重再授权；事件乱序以 `last_event_at`、周期边界和终态保护，状态更新依据业务时间而非“最后到达”。
- 退款、past_due、取消等策略集中在 `BillingService`，前端只读状态。
- 审计事件：支付开关、套餐 / 价格变更、checkout 创建、取消 / 恢复、Webhook 授权变更、管理员手工配额覆盖。
- Waffo API 使用独立 HTTP client、连接超时、响应体上限与结构化错误；日志包含 request ID、provider request ID、状态码和事件 ID，不记录完整 body。
- 生产环境启用 NTP 校时、HTTPS Webhook、按 IP 与 Store 限流；测试环境也走同一套验签逻辑。

## 9. 测试计划

### Go

- `pkg/waffo`：canonical string、RSA-SHA256 签名、PEM / Base64 私钥加载、API 错误映射、超时与响应体上限。
- Webhook：合法签名、篡改 body、过期 / 超前 timestamp、错误 mode / store、重复事件、乱序事件、无效 event ID。
- `httptest` provider：checkout、产品同步、取消 / 恢复全走假服务；CI 使用占位 key，禁止访问真实 Waffo。
- SQLite / PostgreSQL 迁移与 parity：价格唯一键、订阅唯一键、事件去重、租户隔离、管理员手工覆盖保留。
- service：支付关闭、重复 checkout、checkout 过期、订阅激活 / 续期 / 取消 / 逾期 / 退款的配额转换。

### 前端

- `AdminPlansPage`：开关、价格校验、同步状态与错误文案。
- `UsagePage`：套餐展示、月 / 年切换、活动订阅、周期末取消与回跳刷新。
- checkout URL 只被当作导航地址处理，前端状态与 URL 均不保存 buyer token。

## 10. 分阶段交付

1. **基础层**：`pkg/waffo` 签名 / 验签、配置校验、`000019_billing`、repo 与模型。
2. **管理端**：支付开关、价格变体、Waffo 产品同步、审计与 AdminPlansPage。
3. **用户端**：套餐卡、认证 checkout、订阅查询、取消 / 恢复接口。
4. **权益层**：Webhook 异步处理、事件去重、订阅状态机、`tenant_quotas` 套餐来源。
5. **测试环境验收**：注册 HTTPS Webhook，发送 Dashboard 测试事件，使用测试卡覆盖首购、续费、取消、失败、退款，再切换独立生产密钥。

生产打开支付前的检查单：

- Store、产品和价格已发布；
- `WAFFO_ENV=prod` 与 Production API Key / Webhook 公钥匹配；
- Webhook 能从公网 HTTPS 到达当前版本，签名与去重测试通过；
- 免费套餐、管理员覆盖、订阅取消恢复均有回滚路径；
- 审计查询能定位开关、价格、checkout、事件和权益变更。

## 11. 实施记录（2026-09-27）

实现位置：`pkg/waffo`（签名、验签、密钥解析）、`pkg/service/billing_{service,checkout,webhook}.go`、
`pkg/handler/billing_handler.go`、`web/src/components/billing/BillingCard.tsx`、
`web/src/components/admin/BillingAdminPanel.tsx`。以下几条是对照 Waffo 文档与官方 SDK
（`@waffo/pancake-ts` 0.25）核实后与上文初稿不同、或初稿没写到的地方。

**Webhook 负载**

- `currentPeriodStart / End` 是 `2026-03-10` 这样的纯日期，不是 RFC3339。直接解进 `time.Time`
  会让所有订阅事件解析失败，因此按字符串读、两种格式都认。
- 负载里**没有 `productId`**，按产品 ID 反查价格走不通。价格按「结账记录 > `orderMetadata` >
  `productMetadata` > 已有订阅」的顺序取；同步产品时把 `plan_price_id` 写进产品 metadata，
  它随 `productMetadata` 出现在每个订阅事件里。
- 结账页「自愈」重建会话时会丢掉 `orderMetadata` 与 `orderMerchantExternalId`，
  此时租户靠 `merchantProvidedBuyerIdentity`（authenticated checkout 的 buyerIdentity）找回。
- 时间窗与 SDK 一致：过去 45 分钟（重试原样重放签名头）、未来 1 分钟。

**订阅状态机**

- 换套餐在 Waffo 是「旧单取消 + 新单创建」。只有 `activated` / `plan_changed` 能把租户切到新订单；
  旧单迟到的 `renewed`、`canceled` 一律不影响新单。
- 同一订单里，事件时间早于已记录最后事件时间的视为旧事件丢弃。用户在本站点取消 / 恢复时
  `last_event_at` 记为当时，随后到达的更早事件不会把用户刚做的选择翻回去。
- 套餐只在订阅开始或换到新订单时写入 `tenant_quotas`；续费、恢复不重写，
  管理员在订阅期间手工换的套餐不会被续费覆盖，也不会在取消时被回收。

**请求与配置**

- 配置收敛为三个必填变量（见 §3.1）：去掉 `WAFFO_ENABLED`（开关只在后台，它只会造成「写了 true 却没生效」），
  Webhook 公钥内置，回跳地址按请求域名推导。

- 2xx 响应里带 `errors` 同样视为失败（SDK 的 `unwrapAction` 也这样判）。同步失败时 Waffo 的
  错误文案写进 `sync_error`，管理员能看到「Store is not active」之类的具体原因。
- 私钥与公钥接受多行 PEM、`\n` 转义的单行 PEM、去掉头尾的 Base64、整段 PEM 再 Base64。
- 支付环境不由管理员选：API Key 创建时就绑定了 test / prod，`billing_settings.mode` 跟着 `WAFFO_ENV`。
  后台显示当前环境与「打开支付前还缺什么」。
- 产品同步在请求内同步完成（不是初稿写的异步）：管理员保存价格时直接看到同步结果；
  建产品的幂等键绑定本地价格 ID，超时重试不会建出第二个产品。

**测试**：`pkg/waffo`（签名覆盖实际 body、错误信封、时间窗、密钥格式）；
`pkg/service/billing_test.go`（令牌不落库与会话复用、激活 / 取消、同 eventId 不同类型、
换套餐乱序、同单旧事件、管理员套餐不被回收、外来 / 伪造事件、归属冲突、价格规则），
其中乱序与旧事件两条做过变异验证；`api/` 的管理员越权表、租户隔离、未签名 Webhook、支付关闭时的结账；
`pkg/repo` 的计费 ON CONFLICT 语句跨引擎对照；前端 `BillingCard` 两条。

**对账：Webhook 之外的第二条入账路径**（2026-09-27 补上）

本地测试时 Waffo 付款成功，套餐却没变：Webhook 推不到 `localhost`，而入账只有 Webhook 一条路。
§5.3 本来就写了「由订阅查询和 Webhook 最终刷新套餐」，查询这一半一直没做。现在：

- `SyncCheckouts` 取租户最近 24 小时内仍为 `pending` 的结账（最多 5 条），用结账时写入的
  `orderMerchantExternalId` 调 Waffo 只读 GraphQL（`subscriptionOrders`）查订单；订单为
  active / canceling / past_due 就按一次 `subscription.activated` 入账，走与 Webhook 完全相同的
  状态机与乱序保护（事件时间取订单创建时间，之后到达的真实 Webhook 都比它新）。
- 订单生效后结账一律标为 `completed`，包括 Webhook 已先入账、这次被乱序保护跳过的情况；
  `changed` 比较的是对账前后的订阅本身。两处都做错过：前者让结账永远留在待对账列表，
  后者让前端每次都重取、再对账，循环不停。
- 会话过期 5 分钟后仍查不到订单的结账标为 `expired`，之后不再查询。没有待确认结账时不请求 Waffo。
- 用量页每次打开都调一次；**发起结账前也先对账**。测试中出现过：第一笔已付款但本地未入账，
  「已有订阅」的检查放行了第二笔，同一个用户在 Waffo 上背着两份订阅。
- 调用方除租户身份外不提供任何输入，查到的是 Waffo 自己的订单状态，因此与读订阅同一权限、不记审计。

**默认套餐不能标价、不能购买**：它是每个人注册即有、订阅结束后回落的那一档。
创建价格时拒绝；可购列表在 SQL 里排除默认套餐（覆盖此前已标过价的旧数据）；
直接拿旧价格 ID 调结账接口同样拒绝。

**本地开发的实际表现**：付款完成后不会自动回到控制台——Waffo 目前没有自动跳转，买家要在成功页点
「完成」；而本地是 http，回跳地址本就不会带上（Waffo 付款时拒绝非 https）。付完手动回到用量页，
对账会把订阅入账。续费、取消等后续事件仍需要 Webhook：本地要测这些，用 https 隧道把
`/api/v1/webhooks/waffo` 暴露出去，并在 Waffo Dashboard 填上该地址。

**待测试环境验收时确认**（文档没有写清、只能实测）：

1. 生产环境的产品：`publish-product` 文档说产品只能从测试环境发布到生产、且只能发布一次。
   用生产 API Key 直接 `create-product` 是否可行，或必须先在测试环境建好再发布，需要实测后
   决定是否在后台加「发布到生产」动作。
2. 新购会话是否需要 `?test=true`：文档只在换套餐一节写明必需，SDK 的新购路径不加。
   目前测试环境一律加上（文档称它让首屏与回退路径停在测试环境，无副作用）。
3. 已结束订阅的租户再次订阅时，Waffo 是否要求同一 buyerIdentity 走换套餐流程而不是新购。
