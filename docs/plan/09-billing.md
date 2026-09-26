# Waffo 订阅与计费设计

**状态：方案已确认，尚未进入代码实施**

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
3. `tenant_quotas` 继续作为配额计算的唯一入口。订阅变更通过受控 service 更新套餐来源，管理员对单租户的配额覆盖值优先保留。
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
WAFFO_ENABLED=false                 # 启动默认值；最终开关以 billing_settings.enabled 为准
WAFFO_ENV=test                      # test | prod，API Key 本身也绑定环境
WAFFO_API_BASE_URL=https://api.waffo.ai
WAFFO_MERCHANT_ID=MERCHANT_ID
WAFFO_STORE_ID=STORE_ID
WAFFO_PRIVATE_KEY_BASE64=BASE64_PRIVATE_KEY
WAFFO_WEBHOOK_TEST_PUBLIC_KEY=PEM_PUBLIC_KEY
WAFFO_WEBHOOK_PROD_PUBLIC_KEY=PEM_PUBLIC_KEY
```

- 私钥优先使用 secret manager 注入的 Base64 或 PEM 字符串；启动时解码并校验 RSA 密钥。
- Webhook 公钥按 `mode` 选择。Waffo Dashboard 为 Test / Production 各提供一把平台级公钥，配置与事件环境匹配。
- Merchant ID、Store ID 与 API Key 仅由服务端使用，浏览器只收到一次性 checkout URL。
- `WAFFO_ENABLED` 用作配置就绪提示，不作为运行时业务开关。管理员开关保存于数据库并可审计。
- 付款关闭时允许服务启动而无需 Waffo 密钥；管理员打开开关或执行产品同步时，再执行完整配置检查。
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

新增 `000019_billing`，SQLite 与 PostgreSQL 各维护一份 SQL，字段和约束保持同一语义；所有查询带 `tenant_id`，Webhook 事件查询除外且必须以 `store_id + mode` 收敛。

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

保存一次 checkout 意图：`id`、`tenant_id`、`plan_price_id`、`merchant_external_id`、`provider_session_id`、`idempotency_key`、`status`、`expires_at`、时间字段。`idempotency_key` 唯一，重复点击返回同一未过期 checkout URL 或重新生成已过期意图。

### 4.5 `billing_webhook_events`

保存 `event_id`、`event_type`、`store_id`、`mode`、`payload_sha256`、`status`、`error`、接收与处理时间。`event_id + mode` 唯一；原始 payload 只在受控排障存储中保留，应用日志记录摘要和 request ID，避免支付地址与邮箱进入普通日志。

### 4.6 `tenant_quotas` 的套餐来源

给 `tenant_quotas` 增加：

```text
plan_source       admin | subscription
subscription_id   nullable FK tenant_subscriptions(id)
```

订阅激活时写入 `plan_source=subscription`；管理员手工调整时写入 `admin`。取消或到期只回收由该订阅授予的套餐，管理员覆盖值与管理员手工套餐保持原样。这样可以避免 Webhook 把后台刚调整的额度覆盖掉。

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
3. Go 客户端调用 `POST /v1/actions/auth/issue-session-token` 生成一次性 buyer session token，`buyerIdentity` 使用租户 ID；随后调用 `POST /v1/actions/checkout/create-session`。
4. checkout 请求中的 `productId`、`currency`、`buyerEmail`、`successUrl`、`orderMerchantExternalId` 和 `metadata` 全部由服务端根据数据库生成。metadata 至少包含 `tenant_id`、`plan_id`、`plan_price_id`、`checkout_id`。
5. token 仅拼入 Waffo 要求的 URL fragment，不进入日志、数据库、query string 或前端状态；返回 checkout URL 后前端用新标签页打开。
6. 成功回跳页只显示“支付处理中 / 已收到结果”，由订阅查询和 Webhook 最终刷新套餐。付款完成前，额度维持原套餐。

### 5.4 Webhook 验证与处理

Waffo HTTP Webhook 使用 `X-Waffo-Signature: t=<timestamp>,v1=<signature>` 与 `X-Waffo-Event`，body 是 JSON envelope，顶层含 `id`、`eventId`、`eventType`、`storeId`、`mode`、`data`。

处理顺序：

1. 路由读取**原始 body**，限制请求体大小；解析 signature 的 `t` / `v1`。
2. 使用 `timestamp + "." + rawBody` 做 RSA-SHA256 验签；按 `mode` 选择 Test / Production 公钥，并校验时间窗口（默认 45 分钟）。
3. 校验 `storeId`、事件模式、`eventType`、`eventId`，再以 `event_id + mode` 插入去重表。
4. 已处理事件直接返回 2xx；新事件写入 `pending`，交给事务处理器，HTTP 端在 10 秒内返回。
5. 事务按 `eventType` 更新订阅、当前周期与 `tenant_quotas`；事件乱序时以事件时间和当前状态保护旧事件，重复投递保持幂等。

首期处理这些事件：

| 事件 | 权益动作 |
|---|---|
| `subscription.activated` | 建立订阅，切换到价格对应套餐 |
| `subscription.renewed` / `subscription.recovered` | 更新周期，保持订阅套餐 |
| `subscription.payment_succeeded` | 记录付款事件，与 `orderId + periodNumber` 关联续期 |
| `subscription.plan_changed` | 立即生效的变更切换套餐 |
| `subscription.plan_change_scheduled` | 只记录预定变更，当前周期继续使用旧套餐 |
| `subscription.plan_change_failed` | 保持旧套餐并记录失败 |
| `subscription.canceling` / `subscription.uncanceled` | 标记或撤销周期末取消，周期内保留权益 |
| `subscription.past_due` | 标记逾期；按配置保留宽限期 |
| `subscription.canceled` | 到期回收订阅套餐，恢复默认 / 管理员套餐 |
| `refund.succeeded` / `refund.failed` | 记录退款结果；权益策略由 service 的退款规则统一执行 |

无效签名、错误环境、错误 Store ID 返回 4xx 并记安全审计；数据库暂时故障返回 5xx，让 Waffo 重试。普通日志只记录事件 ID、类型、模式和处理结果。

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
| `POST` | `/api/v1/tenants/:tenantID/billing/cancel` | 请求周期末取消 |
| `POST` | `/api/v1/tenants/:tenantID/billing/uncancel` | 撤销周期末取消 |

这些端点沿用现有租户成员中间件与权限矩阵，跨租户 ID 统一返回 404。API Key 只读角色可读取套餐 / 订阅摘要，checkout、取消与恢复需要会话用户的写权限。

### 6.3 Webhook

`POST /api/v1/webhooks/waffo` 是供应商回调入口，采用签名、Store、mode、事件去重和请求限流建立边界。路由需保留原始 body；若 JSON 中间件改变字节序列，验签会失败。

## 7. 前端落点

- `/settings/usage` 改成“套餐与用量”：当前套餐、三项额度、月 / 年切换、价格、订阅状态、周期末取消提示。
- 已有套餐卡继续使用 Kumo `LayerCard`、`Button variant="secondary"`，购买按钮仅在支付开关与价格同步状态满足时出现。
- 购买动作打开 Waffo checkout 新标签页；回跳后刷新 `subscription` 与 `quota`，不在 URL 中保存 token 或私密字段。
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
