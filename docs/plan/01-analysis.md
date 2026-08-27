# 01 · 现状分析

## 1. emailbox 模板现状

### 1.1 已有能力

模板（`go.mod` 模块名仍为 `go-react-template`）是一个可运行的全栈骨架：

**后端**（Go 1.25 + Echo v5）

```
main.go                 启动、中间件装配、优雅关机、SPA 静态文件、过期会话清理协程
api/routes.go           路由注册（/api/v1），限流、租户中间件、权限中间件
configs/config.go       环境变量配置
pkg/database/           sql.DB 初始化（sqlite: modernc.org/sqlite；postgres: pgx stdlib）
db/migrations/          版本化迁移，sqlite / postgres 各一套
db/query/               手写 SQL，sqlc 生成到 db/generated/{sqlite,postgres}
pkg/repo/               Store：按 driver 分派到两套生成代码，WithTx 事务封装
pkg/service/            业务逻辑
pkg/handler/            HTTP 解析 + 统一响应 {code,data,message}
pkg/middleware/         session.go（会话鉴权）、tenant.go（租户成员校验 + 权限）
pkg/model/              领域模型 + DTO + 权限表
```

现有业务实体只有 4 张表：`users`、`tenants`、`tenant_members`、`sessions`。
权限模型是 `role → permission set` 的静态映射（`pkg/model/permission.go`），
路由上用 `middleware.Require(model.PermissionXxx)` 声明。

**前端**（React 19 + Vite 8 + TS + Tailwind 4 + Zustand + React Router 7）

```
web/src/api/            按资源分文件的 API 封装，返回 ApiResponse<T>
web/src/lib/client.ts   axios 实例，withCredentials，401 → triggerUnauthorized
web/src/store/          authStore / tenantStore
web/src/router/         createBrowserRouter + ProtectedRoute/PublicRoute
web/src/pages/          登录、注册、Dashboard、租户设置、成员管理等
```

`components.json`（shadcn 配置）已存在，但 `components/ui/` 尚未生成任何组件。
**本项目改用 Cloudflare Kumo，该配置文件与 `style.css` 里的 shadcn 风格令牌都将被移除**
（见 [06 文档 §1.3](06-frontend.md)）。

### 1.2 需要为本项目调整的既有实现

这些是模板当前写法与「批量邮箱平台」诉求直接冲突的点，实施前必须处理：

1. **`main.go` 的 `middleware.BodyLimit(64 * 1024)` 是全局的。**
   批量导入一次可能提交上万行 `邮箱----密码----client_id----refresh_token`，
   64KB 会直接 413。需要保留全局小上限，仅对导入类路由单独放宽（见 05 文档）。

2. **CORS `AllowMethods` 没有 `PUT`。** 现有路由只用 PATCH，新增功能若用 PUT 会被预检拦掉。
   方案：统一只用 PATCH（推荐，与现有风格一致），或补齐 PUT。

3. **模块名 `go-react-template`。** 应改名为 `emailbox`，涉及全仓库 import 路径替换。

4. **没有任何"加密存储"设施。** 邮箱密码、refresh_token、IMAP 授权码
   全是高敏数据，必须引入应用层加密（见 02 文档）。

5. **没有后台任务设施。** 目前只有 `purgeExpiredSessions` 一个裸 goroutine。
   Token 批量刷新需要真正的任务模型（任务表 + worker pool + SSE），见 02 文档 §4。

6. **SQLite 连接池限制为单连接**（`repo.Store` 注释中提到 `MaxOpenConns=1`）。
   批量场景下 SQLite 是明确的写入瓶颈，生产部署应推荐 PostgreSQL。

7. **`db/query/` 每条 SQL 要写两遍**（sqlite + postgres）。新增约 60~80 条查询意味着
   120~160 段 SQL。这是模板既定的架构代价。方案不改变双驱动策略，并明确
   **两个引擎各写各的最优 SQL**（不强求写法一致），靠跨引擎对照测试防语义漂移
   （见 [03 文档 §5.1](03-data-model.md)）。

8. **`AuthService.Register` 只创建 user。** SaaS 形态下注册必须事务化地同时创建
   个人租户、成员记录、默认邮箱分组与配额记录，否则新用户登录后处处 403 且无法自助修复。

9. **会话中间件不校验 `users.status`。** 模板 `users` 表已有 `status` 列，但鉴权路径没读它，
   导致「管理员禁用用户」无法真正生效（已登录会话仍可用）。这是管理功能的前置修复。

---

## 2. outlookEmail 现状

### 2.1 代码组织

Python 侧被拆成 11 个 "segment" 文件，在 `outlook_web/app.py` 里按顺序拼接执行，
本质是一个 2.5 万行的单文件 Flask 应用：

| 文件 | 行数 | 职责 |
|------|------|------|
| `01_bootstrap.py` | 3463 | 常量、provider 表、加密、DB 初始化与自迁移、设置、皮肤、登录会话 |
| `02_groups_accounts.py` | 3770 | 分组树、账号 CRUD/查询/批量、别名、代理解析、项目（账号领取）、导入解析 |
| `03_mail_helpers.py` | 2924 | **协议核心**：代理、Graph API、IMAP(OAuth/密码)、MIME 解析、附件 |
| `04_routes_groups_accounts.py` | 1608 | 分组/账号/标签/导出/批量操作路由 |
| `05_routes_refresh_mail.py` | 4403 | Token 刷新（含 SSE 流式）、刷新日志、邮件删除/标记已读 |
| `06_routes_temp_email.py` | 2876 | 临时邮箱（GPTMail / DuckMail / Cloudflare）**不移植** |
| `07_routes_oauth_settings_external.py` | 1742 | OAuth 助手、系统设置、对外 API（**只移植 OAuth 助手**） |
| `08_forwarding_scheduler_errors.py` | 1883 | 邮件转发（SMTP/TG/企微）、APScheduler、WebDAV 备份**均不移植** |
| `09_routes_system_update.py` | 722 | Docker 在线更新（Watchtower） |
| `10_routes_email_shares.py` | 500 | 邮件分享链接 **不移植** |
| `11_routes_graph_oauth.py` | 706 | 自动化 Graph 授权（模拟登录抓取 refresh_token） |

### 2.2 必须借鉴的部分（高价值）

**A. 三条读信链路与回退顺序**（`03_mail_helpers.py`）

这是整个项目最有价值的资产，是大量真实账号试出来的：

- Outlook OAuth → **Graph API**（首选）→ **IMAP 新版** `outlook.live.com` → **IMAP 旧版** `outlook.office365.com`
- 三个不同的 token 端点，scope 各不相同，不可混用
- 成功通道会记回 `accounts.authorization_type`，下次优先走它

**B. Graph token 的 scope 候选降级**（`request_graph_token_response`）

依次尝试 `configured`（显式委托 scope）→ `read`（去掉 Mail.ReadWrite）→ `.default`，
并通过检测 `AADSTS90023 / AADSTS70000 / AADSTS70011 / invalid_scope / consent` 决定是否降级重试。
这个逻辑解决的是「不同来源账号授权范围不一致」的现实问题，必须完整保留。

**C. IMAP 文件夹解析**（`resolve_imap_folder` 及相关）

各家 IMAP 的垃圾箱/已删除名称完全不同（`PROVIDER_FOLDER_MAP`），
还涉及 **IMAP modified UTF-7** 解码（如 `&V4NXPpCuTvY-` = 垃圾邮件），
以及先 `LIST` 再按别名集合打分排序的兜底策略。

**D. 代理体系**

- 分组代理 → 子分组继承 → 账号代理覆盖
- 主代理 + 2 个 fallback，按顺序试，识别「是否值得换下一个代理重试」
- `{mail}` 占位符：替换为邮箱 local-part 去非字母数字并小写（对接 Resin 粘性代理池）
- HTTP 请求走 `requests` 的 proxies；IMAP 走 SOCKS

**E. 账号导入格式与解析**

```
Outlook OAuth: 邮箱----密码----client_id----refresh_token
               邮箱----密码----refresh_token----client_id   （顺序自动识别）
标准 IMAP:     邮箱----授权码
自定义 IMAP:   邮箱----密码----imap_host----imap_port
```
`is_probable_client_id()` 用 UUID 形态判断第 3/4 段谁是 client_id。

**F. 别名与邮箱匹配规则**

- 主邮箱 + 别名表都能命中同一账号
- `user+tag@example.com` 的 `+` 别名回退到主邮箱
- `@gmail.com` / `@googlemail.com` 互为回退后缀

**G. 数据模型的业务字段**

`accounts` 表上的 `last_refresh_status / last_refresh_error / refresh_token_updated_at /
authorization_type` 等，是批量运维的核心可观测字段。

### 2.3 要改造的部分（不照搬）

| outlookEmail 做法 | 问题 | emailbox 做法 |
|---|---|---|
| IMAP 代理靠 `socks.set_default_proxy()` + 全局 `socket.socket` 替换 + 进程锁 | **所有 IMAP 连接被强制串行**，批量场景的最大瓶颈 | Go 每连接独立 `proxy.Dialer`，无全局状态，可并发 |
| Fernet + 固定盐 `outlook_email_encryption_salt_v1` | 固定盐、无密钥轮换路径 | AES-256-GCM，每记录随机 nonce，密文带密钥版本前缀 |
| 运行时 `ALTER TABLE` 逐列补齐 | 无版本、无回滚、无法审计 | 版本化迁移文件（模板已有 `db/migrations` 机制） |
| 必须单 worker（SSE 任务状态放进程内存） | 无法水平扩展 | 任务状态入库；SSE 从任务表/事件表读，多实例只需调度选主 |
| 单登录密码 + 全局 API Key | 无多用户、无审计归属 | 复用模板租户体系；**对外 API 整体不做**（见 07 文档 §5） |
| 皮肤系统（zip/git 安装 CSS） | 与核心目标无关，且引入 zip 解压/git 拉取攻击面 | **不移植** |
| Docker 在线更新（挂载 docker.sock） | 把宿主 Docker 管理权交给 Web 应用，风险高 | **不移植**，用常规镜像发布流程 |
| 浏览器扩展 | 依赖对外 API | **不做** |
| `11_routes_graph_oauth.py` 模拟登录抓 token | 依赖微软登录页 HTML 结构，极易失效且合规风险高 | **不移植**，只保留标准 OAuth 授权码助手 |

---

## 3. 结论

借鉴范围收敛为四块：**协议链路**（04 文档）、**数据模型的业务字段**（03 文档）、
**批量运维语义**（05/06 文档）、**代理与导入格式**（04 文档）。
其余按当前模板的分层、迁移、鉴权体系重新设计。
