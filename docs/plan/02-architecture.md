# 02 · 目标架构

## 1. 包结构

在模板既有分层（handler → service → repo → sqlc）之上，新增两类包：
**协议适配层 `pkg/mailer/`**（无状态、不碰数据库）和 **任务层 `pkg/job/`**。

```
emailbox/
├── main.go                      装配：配置 → DB → 迁移 → Store → Service → Handler → 调度器 → HTTP
├── api/
│   └── routes.go                路由注册（新增 mail 域路由组）
├── configs/
│   └── config.go                新增 Crypto / Mail / Job 配置段
├── db/
│   ├── migrations/{sqlite,postgres}/  000002_mail.up.sql 起的新迁移
│   ├── query/{sqlite,postgres}/       按功能域拆文件
│   └── generated/{sqlite,postgres}/   sqlc 产物
├── pkg/
│   ├── crypto/                  ★新增 AES-256-GCM 信封加密、密钥版本、Hash 工具
│   ├── mailer/                  ★新增 协议适配层（本方案核心）
│   │   ├── mailer.go            Client 接口 + Message/Detail/Attachment 领域类型
│   │   ├── provider.go          MAIL_PROVIDERS 表、域名推断、文件夹候选表
│   │   ├── credential.go        Credential 结构（解密后的凭据，仅内存流转）
│   │   ├── proxy.go             代理解析、{mail} 模板、failover 候选、Dialer 构建
│   │   ├── graph/               Microsoft Graph 实现
│   │   │   ├── token.go         scope 候选降级 + AADSTS 判定
│   │   │   ├── client.go        列表/详情/附件/标已读/删除
│   │   ├── imapx/               IMAP 实现（OAuth XOAUTH2 与 密码 两种鉴权）
│   │   │   ├── conn.go          连接、代理拨号、IMAP ID、鉴权
│   │   │   ├── folder.go        文件夹解析、UTF-7、LIST 排序
│   │   │   ├── fetch.go         列表/详情/附件、UID vs 序列号
│   │   │   └── mime.go          MIME 解析、正文/HTML 提取、附件枚举
│   │   ├── chain.go             ★通道回退链（Graph → IMAP新 → IMAP旧）
│   │   └── errors.go            结构化错误（含 banned / auth_failed / proxy_failed 分类）
│   ├── job/                     ★新增 任务框架
│   │   ├── manager.go           提交、查询、取消、心跳、断线续看
│   │   ├── worker.go            worker pool、限速、逐账号结果落库
│   │   └── stream.go            SSE 事件广播 + 事件表回放
│   ├── quota/                   ★新增 SaaS 配额：生效值计算、用量计数、CheckAndConsume
│   ├── repo/                    新增 groups/accounts/aliases/messages/jobs/quota/...
│   ├── service/                 新增 group/account/mail/refresh/admin/... service
│   ├── handler/                 对应 handler（含 admin_handler.go）
│   ├── middleware/              新增 platform.go（RequirePlatformAdmin）
│   └── model/                   新增领域模型 + 扩展 permission.go + platform_role
└── web/                         前端（见 06 文档）
```

**依赖方向铁律**：`mailer` 不 import `repo`/`service`；`service` 负责「从库里取账号 → 解密 →
组装 `mailer.Credential` → 调用 mailer → 把结果写回库」。这样协议层可以独立单测，
也可以在没有数据库的情况下用真实账号做联调工具。

## 2. 新增第三方依赖

| 依赖 | 用途 | 备注 |
|---|---|---|
| `github.com/emersion/go-imap/v2` | IMAP 客户端 | v2 仍在 beta；接口比 v1 干净且支持 `UID FETCH`、`ID` 扩展。若追求稳定可退回 v1 + 手写 `ID` 命令 |
| `github.com/emersion/go-sasl` | XOAUTH2 SASL 机制 | go-imap 配套 |
| `github.com/emersion/go-message` | MIME 解析（含 `mail` 子包） | 替代 Python `email` 模块，处理编码头、multipart、附件 |
| `golang.org/x/net/proxy` | SOCKS5 Dialer | `socks5h` 语义（远端 DNS）是其默认行为 |

`golang.org/x/crypto` 已在 go.mod（bcrypt）；AES-GCM 用标准库 `crypto/aes` + `crypto/cipher`。

> 依赖数量从 6 个直接依赖涨到 10 个左右。这是引入真实邮件协议栈的必然成本，
> 但每一个都只在 `pkg/mailer` 内部使用，不会渗透到 handler/service 层。

## 3. 敏感数据加密（`pkg/crypto`）

### 3.1 需要加密的字段

| 表.列 | 内容 |
|---|---|
| `mail_accounts.password_enc` | 邮箱登录密码 |
| `mail_accounts.refresh_token_enc` | OAuth refresh_token |
| `mail_accounts.imap_password_enc` | IMAP 授权码 / 应用专用密码 |
| `mail_accounts.proxy_url` 系列 | 含代理认证口令 → 整串加密 |

### 3.2 设计

```go
// pkg/crypto/cipher.go
// 密文格式： "enc:v1:" + base64url( nonce(12B) || ciphertext || tag(16B) )
// - 前缀带版本号，为将来密钥轮换保留空间（v2 可用新密钥，解密按前缀分派）
// - 每条记录独立随机 nonce，杜绝 GCM nonce 复用
type Cipher interface {
    Encrypt(plaintext string) (string, error)
    Decrypt(ciphertext string) (string, error)
    IsEncrypted(s string) bool
}
```

- 密钥来源：环境变量 `ENCRYPTION_KEY`（32 字节的 base64 或 hex）。
  **不从 SESSION/SECRET_KEY 派生**——两者生命周期不同，会话密钥轮换不应导致全部账号无法解密。
- 启动时若 `ENCRYPTION_KEY` 缺失：`sqlite` 开发模式打警告并允许明文存储（便于本地调试），
  生产模式（`APP_ENV=production`）直接 fatal 退出。这一分支必须在 `configs.Init()` 里显式实现。
- 解密失败必须返回明确错误（密钥变更/数据损坏），**不要静默返回空串** —— 静默失败会导致
  批量刷新把上万个账号标记为「令牌无效」，是灾难性的误判。
- 提供 `HashToken(s) string`（SHA-256 hex）用于令牌类字符串的查表哈希，
  与模板 `sessions.token_hash` 的做法保持一致。

### 3.3 与 outlookEmail 的差异

Python 用固定盐 PBKDF2 从 `SECRET_KEY` 派生 Fernet 密钥。本方案改为独立密钥 + 随机 nonce +
版本前缀，密钥轮换靠密文的版本前缀分派，轮换脚本按需再写。

## 4. 并发与任务模型

### 4.1 为什么必须有任务表

批量刷新 5000 个账号，单账号平均 1.5s，即使 20 并发也要 6 分钟。这期间：
浏览器会断线、用户会刷新页面、服务可能重启。outlookEmail 把进度放进程内存并因此
**强制单 worker 部署**。本方案把任务状态入库：

```
jobs        一条任务的聚合状态（total/success/failed/status/started_at/heartbeat_at）
job_items   每个账号的处理结果（account_id, status, error, finished_at）
job_events  按序号递增的事件流，供 SSE 断线重连后从 last_event_id 回放
```

### 4.2 执行模型

```
Service.SubmitRefreshJob(ctx, tenantID, accountIDs)
  → 写 jobs + job_items(pending)
  → 投递到 job.Manager 的内存队列
  → worker pool（默认 N=8，可配）逐个取 item：
        解密凭据 → mailer.RefreshToken → 写回 accounts.refresh_token_enc/last_refresh_*
        → 更新 job_items + 累加 jobs 计数 + 追加 job_event
  → 全部完成 → jobs.status = succeeded/partial/failed
```

- **限速**：账号间可配 `JOB_ACCOUNT_DELAY_MS`（对应 Python 的 `refresh_delay_seconds`），
  避免触发微软风控。
- **停止**：`jobs.status = stopping`，worker 每个 item 前检查一次（对应 Python 的
  `request_token_refresh_stop`）。
- **心跳**：`jobs.heartbeat_at` 每 5s 更新；启动时把「心跳超过 2 分钟且仍是 running」
  的任务标为 `interrupted`，防止崩溃后留下永远 running 的僵尸任务。

### 4.3 多实例

**按单实例设计**，并在部署文档中写明。日后若需要水平扩展：

- PostgreSQL：`SELECT ... FOR UPDATE SKIP LOCKED` 取 job_items，多实例天然可并行。
- SQLite：不支持多实例，部署文档明确写「SQLite 仅限单实例/开发」。

### 4.4 没有 cron 调度器

原方案里有一个 `pkg/job/scheduler.go`（定时刷新 Token、轮询转发、归档日志、WebDAV 备份），
随转发功能一起删掉了（[07 文档 §5](07-roadmap.md)）。**任务一律由用户手动触发。**
唯一的周期性动作仍是模板自带的过期会话清理 goroutine（`purgeExpiredSessions`），
它不需要 cron，也不需要 `robfig/cron` 依赖。

## 5. 配置扩展（`configs/config.go`）

在现有 `Server` / `Database` / `Session` 之外新增：

```go
type CryptoConfig struct {
    Key      string // ENCRYPTION_KEY，32 字节 base64/hex
    Required bool   // APP_ENV=production 时强制 true
}

type MailConfig struct {
    HTTPTimeout        time.Duration // HTTP_REQUEST_TIMEOUT，默认 30s
    IMAPTimeout        time.Duration // IMAP_TIMEOUT，默认 45s
    OverallTimeout     time.Duration // MAIL_FETCH_OVERALL_TIMEOUT，默认 max(两者)+5s
    OAuthClientID      string        // OAUTH_CLIENT_ID，默认沿用公共客户端 ID
    OAuthRedirectURI   string        // OAUTH_REDIRECT_URI，默认 http://localhost:8080
    DefaultPageSize    int           // 每页邮件数，默认 20
    MaxPageSize        int           // 上限 50（与 outlookEmail 对外 API 一致）
}

type JobConfig struct {
    Workers          int           // JOB_WORKERS，默认 8
    AccountDelay     time.Duration // JOB_ACCOUNT_DELAY_MS，默认 0
    QueueSize        int           // 默认 1024
    HeartbeatTimeout time.Duration // 默认 2m
    EventRetention   time.Duration // job_events 保留期，默认 7d
}

type SaaSConfig struct {
    RegistrationMode   string // REGISTRATION_MODE：open | invite | closed，默认 open
    BootstrapAdminUsername string // BOOTSTRAP_ADMIN_USERNAME：启动时把该用户提为管理员
    DefaultPlanCode    string // DEFAULT_PLAN_CODE，默认 "free"
}
```

`.env.example` 同步补齐并加中文注释，风格与现有文件一致。

## 6. 可观测性

- 沿用 `log/slog` JSON 结构化日志。协议层的关键日志字段统一为：
  `tenant_id`、`account_id`、`email`（**打码**，如 `u***@outlook.com`）、`channel`（graph/imap_new/imap_old）、
  `proxy`（口令打码，复用 Python 的 `format_proxy_for_log` 思路）、`duration_ms`、`error_kind`。
- **绝不记录** refresh_token、密码、access_token、完整代理 URL。
- `audit_logs` 表记录写操作（导入、删除、导出、改设置），带 `actor_user_id` 与 IP。
- 曾计划的 `/api/v1/metrics/summary`（租户内账号数、失败账号数、最近刷新任务状态）未实现；
  `/api/v1/health` 已删除——静态探针测不出任何依赖故障，理由见 [docker.md](../docker.md)。
  当前可观测性只有结构化日志与 `audit_logs`。

### 2.1 `github.com/lib/pq` 的特殊说明

`go.mod` 里有 `github.com/lib/pq`，但**它不是数据库驱动**——连接始终走 `pgx/v5/stdlib`。

引入它的唯一原因是 sqlc 在 `sql_package: database/sql` 模式下，为 PostgreSQL 的
数组参数（`= ANY($n::text[])`，见 [03 文档 §5.1](03-data-model.md)）生成 `pq.Array(...)` 包装。
`pq.Array` 只是把 `[]string` 编码成 `{"a","b"}` 这种数组字面量再作为文本参数发出，
SQL 里的 `::text[]` 负责转回数组，pgx 对此完全兼容。

要去掉这个依赖只能把 sqlc 切到 `sql_package: pgx/v5`，那会改变整个生成包的
连接句柄类型，牵连 `pkg/database` 与全部 repo 方法——代价远大于收益。
