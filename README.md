# Emailbox · 批量邮箱托管平台

面向公众注册的多租户 SaaS：批量托管第三方邮箱账号（Outlook OAuth / Gmail / QQ / 163 等），
统一收信、刷新令牌、配代理、做批量运维。注册即获得独立工作空间，租户之间完全隔离；
平台管理员可跨工作空间管理，且每一次跨租户访问都留审计。

功能与协议链路参考同仓库的 Python 项目 `outlookEmail/`（未纳入版本控制，仅作实现依据），
但不是移植：协议层用 Go 重写，每条连接独立 Dialer，因此批量拉信是**真并发**而非串行。
完整设计见 [`docs/plan/`](docs/plan/README.md)。

## 能力

**邮箱业务**

- 分组、别名、备注，账号可批量移动/停用/删除
- 批量导入：三种文本格式自动识别（4 段 Outlook OAuth、2 段授权码、4 段自定义 IMAP），
  逐行报错、分批事务、超配额部分计入 skipped 而非整批失败
- 导出：`account:secret` 权限 + 强制审计 + 按用户限流，导出文件可原样重新导入
- 收信：Microsoft Graph → IMAP(新) → IMAP(旧) 三通道回退链，成功通道写回账号
- 邮件正文经 DOMPurify 净化后在 sandbox iframe 内渲染，远程图片默认阻断
- 对外取件 API：一个工作空间一把只读 Key（`Authorization: Bearer`），
  配套公开的 `/llms.txt` 供 Agent 自助接入
- SOCKS5 / HTTP 代理，支持 `{mail}` 模板变量与两级 failover
- Token 批量刷新：任务表 + worker pool + SSE 进度，可停止、可断线续看，
  进程被强杀后遗留任务在下次启动被标为 `interrupted`

**SaaS 与后台**

- 套餐与配额（账号数、分组数、每日拉信次数），超额返回 `code=1001`；
  令牌刷新只记用量、不设上限
- 平台管理员：用户管理、套餐管理、租户配额覆盖、跨租户邮箱管理、审计查询
- 审计：全部写操作 + 管理员的三类读操作（看账号列表、看邮件正文、导出）

**安全**

- 邮箱密码、`refresh_token`、含口令的代理地址均以 AES-256-GCM 加密落库（`enc:v1:` 前缀）
- 会话 token 只存 SHA-256 哈希；禁用用户后其会话立即失效
- 每类资源都有跨租户越权测试，每个 `/admin/*` 端点都有非管理员 403 测试

## 技术栈

- Go 1.25、Echo v5、`database/sql` + sqlc（无 ORM）、`log/slog`
- SQLite（开发）与 PostgreSQL（生产）双引擎，**两套 SQL 各写各的**，靠跨引擎对照测试防漂移
- go-imap/v2（自实现 XOAUTH2）、Microsoft Graph REST
- React 19、TypeScript、Vite、Tailwind CSS v4、[Cloudflare Kumo](https://kumo.cloudflare.com)、Zustand
- bcrypt、HttpOnly Cookie Session

## 快速开始

```bash
cp .env.example .env
make deps
make gen-key           # 把输出的 ENCRYPTION_KEY 写进 .env
make dev               # 后端 :1323（Air 热重载）+ 前端 :5173（Vite）
```

默认用 SQLite（`app.db`），开箱即用。`.env.example` 里预置了一个管理员，
首次启动会自动建号：

```env
BOOTSTRAP_ADMIN_USERNAME=admin
BOOTSTRAP_ADMIN_PASSWORD=admin123..
```

**登录后立刻改密码，并把 `BOOTSTRAP_ADMIN_PASSWORD` 删掉**——这个密码写在仓库的示例文件里。
想用已有账号当管理员就只填用户名：用户存在即提权，且不会动他的密码。

单二进制方式运行（前端产物会被复制进 `static/`，由 Go 一并伺服）：

```bash
./scripts/build.sh && ./server        # http://localhost:1323
```

切换 PostgreSQL：

```env
DB_DRIVER=postgres
DB_HOST=localhost
DB_PORT=5432
DB_USERNAME=postgres
DB_PASSWORD=password
DB_NAME=emailbox
DB_SSLMODE=disable
```

## 部署到生产前

以下几项的默认值只适用于本地开发：

```env
APP_ENV=production            # 缺 ENCRYPTION_KEY 时由「启动告警」升级为「启动失败」
ENCRYPTION_KEY=<make gen-key> # 丢失后已存凭据无法恢复，务必备份；示例文件里那个是公开的，必须换
BOOTSTRAP_ADMIN_PASSWORD=      # 建完号就清空，别把它留在生产环境里
COOKIE_SECURE=true            # HTTPS 部署必须，否则会话 Cookie 可能明文传输
CORS_ALLOW_ORIGINS=https://app.example.com   # 不接受 *，非法配置启动即报错
TRUST_PROXY=true              # 在 Nginx / 云负载均衡后面时必须，否则限流会把所有人当成同一个 IP
```

生产建议用 PostgreSQL：SQLite 只有一个写连接，批量导入与刷新会串行。
完整配置项见 [`docs/configuration.md`](docs/configuration.md)。

本平台托管的是**第三方邮箱凭据**，敏感度高于自用工具：服务条款须要求用户保证对托管账号
拥有合法授权，注册页与导入页各展示一次提示；导出与管理员跨租户访问这两条数据出口
全部走审计。

## 开发约定

**改数据库**：在 `db/migrations/sqlite/` 与 `db/migrations/postgres/` 下各放一个同版本号的文件，
启动时按版本号顺序执行未应用的迁移。

**改查询**：先改 `db/query/{sqlite,postgres}/` 下的 SQL，再 `make sqlc-generate`；CI 会校验生成结果是否最新。
两条硬性约束：

- `db/query/` 下的 SQL **必须全部 ASCII**——sqlc 遇到多字节字符会静默截断生成的 SQL 常量，
  且爆炸点常在另一条查询上（`db/query/query_test.go` 拦这个）
- `ORDER BY` 无法参数化，每个「排序字段 × 方向」各写一条查询，由 service 按白名单分派

**写测试**：只测核心行为与安全边界，不做防御性堆砌；新增用例优先并进已有文件。
租户隔离与平台角色隔离这两类测试不可省。详见 [07 文档 §1](docs/plan/07-roadmap.md)。

## 常用命令

```bash
make dev             # 前后端开发环境
make sqlc-generate   # 由 db/query/ 下的 SQL 生成代码
make sqlc-verify     # 校验生成代码是否最新
make gen-key         # 生成 ENCRYPTION_KEY
make test            # 前后端测试
make lint            # 前后端代码检查
./scripts/build.sh   # 构建单二进制 + static/
go run ./cmd/mailprobe -h   # 逐通道诊断某个邮箱账号能不能收信
```

## 文档

- [配置说明](docs/configuration.md) — 全部环境变量
- [Docker 部署](docs/docker.md) — 镜像构建、生产配置与已知限制
- [开发方案](docs/plan/README.md) — 架构、数据模型、协议层、API、前端、路线图、SaaS 后台、两篇改版记录
- [实施进度](docs/plan/PROGRESS.md) — 各阶段完成状态与实现过程中踩到的坑
- [Go 代码检查](docs/golangci-lint.md) · [Air 热重载](docs/air.md)

## 实现进度

P0 地基 / P1 分组与账号 / P2 邮件协议层 / P3 管理后台 / P4 任务系统与 Token 刷新 —— 均已完成，
**这就是全部范围**。原先规划的 P5 转发与调度、P6 对外 API 与分享、P7 增强（临时邮箱、
邀请码、多实例、计费）已于 2026-08-21 从方案中删除，见
[07-roadmap.md §5](docs/plan/07-roadmap.md)。当初为这些功能预埋的字段、路由与配置项
已于 2026-08-25 随 `000006_drop_unused` 清理干净——代码里现在没有点了不生效的开关。

其中「对外 API」这一条在 2026-08-27 以**只读取件 Key** 的形式重新做了一小块：
没有独立路由、一租户一把、权限固定为读分组/读账号/读邮件，与原 P6 的设计不是一回事
（对照见 [07-roadmap.md §5.1](docs/plan/07-roadmap.md)）。同日分组从三级树压平成一层。
逐项状态见 [PROGRESS.md](docs/plan/PROGRESS.md)。

## 目录

```text
api/                 路由装配与接口级测试（越权 / 403 / 大请求体）
cmd/mailprobe/       协议层诊断 CLI：逐通道单独试，不走回退链
configs/             环境变量配置
db/migrations/       SQLite / PostgreSQL 迁移，两套同版本号
db/query/            sqlc 命名 SQL，两个方言各一份
db/generated/        sqlc 生成代码
pkg/crypto/          AES-256-GCM 凭据加解密
pkg/quota/           配额计算与消耗
pkg/mailer/          邮件协议层：graph/ 与 imapx/，回退链、代理、导入导出格式
pkg/job/             任务系统：Manager、worker pool、事件广播
pkg/handler/         HTTP 处理器与审计中间件
pkg/service/         业务逻辑
pkg/repo/            数据访问层，在此适配两种方言
pkg/middleware/      认证（会话 Cookie / API Key）、租户、平台管理员中间件
pkg/model/           数据结构与 RBAC 权限矩阵
web/                 React 前端（Kumo 组件 + 语义令牌）
```
