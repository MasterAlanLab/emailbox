# 配置

项目通过环境变量配置，示例见根目录 `.env.example`。

## 运行模式

```env
APP_ENV=development
```

`production` 下若干安全项会从「启动告警」升级为「启动失败」，目前包括 `ENCRYPTION_KEY`。

## 服务监听

```env
SERVER_HOST=0.0.0.0
SERVER_PORT=1323
```

单进程部署，Go 同时伺服 API 与 `static/` 下的前端产物。

## SQLite

```env
DB_DRIVER=sqlite
DB_PATH=app.db
```

## PostgreSQL

```env
DB_DRIVER=postgres
DB_HOST=localhost
DB_PORT=5432
DB_USERNAME=postgres
DB_PASSWORD=password
DB_NAME=emailbox
DB_SSLMODE=disable
```

## Session

```env
SESSION_EXPIRE_HOUR=24
# HTTPS 部署必须设为 true，否则会话 Cookie 可能被明文传输
COOKIE_SECURE=false
```

会话 token 为随机生成，服务端只保存其 SHA-256 哈希，因此不需要额外的签名密钥。
生产环境请通过 HTTPS 提供服务并设置 `COOKIE_SECURE=true`。

### 为什么不需要 CSRF token

会话 Cookie 带 `SameSite=Lax`：浏览器不会在跨站的 `POST/PATCH/DELETE` 上附带它，
而本项目所有写操作都用这三个方法（不用 GET 改状态）。这已经堵住了 CSRF 的入口，
再加一层 token 只增加复杂度。

这个结论依赖两个前提，**破坏其中任何一个都必须重新引入 CSRF token**：

1. 不出现「用 GET 触发写操作」的接口——`SameSite=Lax` 对跨站的顶层 GET 导航是放行的
2. 前端与后端处于同一个可注册域（如 `app.example.com` 与 `api.example.com`）。
   若拆到不同顶级域部署，`Lax` 会连正常请求的 Cookie 也一并挡掉，
   届时改用 `SameSite=None; Secure`，而那时就**必须**配 CSRF token

## CORS

```env
# 允许跨域访问的前端来源，逗号分隔
CORS_ALLOW_ORIGINS=http://localhost:5173,http://localhost:3000
```

来源列表不能为空，也不接受 `*`（会话使用 Cookie 凭证，通配来源不被允许）。
配置非法时服务会在启动阶段报错退出。

## 敏感字段加密

```env
# 32 字节密钥的 base64 或 hex，用 `make gen-key` 生成
ENCRYPTION_KEY=
```

`.env.example` 里带了一个**示例密钥**（解开来是 `example-key-do-not-use-in-prod!!`），
只为让人 `cp .env.example .env` 之后能直接跑起来。它写在仓库里、谁都看得见，
用它等于没加密——任何真实部署都必须 `make gen-key` 换成自己的。

邮箱账号的登录密码、OAuth `refresh_token`、含认证信息的代理地址等在落库前用
AES-256-GCM 加密，密文格式为 `enc:v1:` + base64url(nonce ‖ 密文 ‖ tag)，每条记录使用独立随机 nonce。

- 密钥**独立于会话密钥**：两者生命周期不同，轮换会话密钥不应导致全部邮箱账号无法解密。
- 留空时：开发模式打告警并明文存储（便于本地调试），`APP_ENV=production` 直接启动失败。
- 解密失败会返回明确错误而非空串——静默失败会让批量刷新把上万个账号误判为「令牌无效」。
- 密钥丢失后已存凭据无法恢复；轮换密钥需要先用旧密钥解密再用新密钥加密。

## 注册与平台管理员

```env
# open（任何人可注册）/ closed（仅管理员创建）
REGISTRATION_MODE=open

# 启动时把该用户名对应的用户提升为平台管理员
BOOTSTRAP_ADMIN_USERNAME=admin
# 该用户还不存在时，用这个密码把他建出来（连同个人工作空间、默认分组、配额）
BOOTSTRAP_ADMIN_PASSWORD=admin123..

# 新注册租户默认套餐的 code
DEFAULT_PLAN_CODE=free
```

不采用「第一个注册的人自动成为管理员」——那在开放注册的平台上是明显的抢注漏洞。

两条规则：

- **只填用户名**：用户不存在时什么都不做，等他注册后下次启动生效
- **填了密码**：用户不存在时直接建号并提权（绕过 `REGISTRATION_MODE=closed`，
  这是部署者在自己机器上开门，不是公开注册）。**用户已存在时不会改他的密码**——
  配置文件不该能悄悄接管一个已有账号，也不该在用户改完密码后于下次重启静默改回去

`.env.example` 里的 `admin` / `admin123..` 是为了 `cp .env.example .env` 之后后台直接能进。
它写在仓库里，等于公开凭据：**建号成功后立刻登录改密码，并把 `BOOTSTRAP_ADMIN_PASSWORD`
从环境里删掉**。启动日志里的那条 WARN 就是提醒这件事的。

认用户名而不是邮箱：邮箱是可选字段，按邮箱找的话，一个所有人都没填邮箱的部署
将永远产生不出管理员，后台也就永远进不去。用户名两端的空白会被忽略，
但**大小写敏感**——用户名是原样存的，这里擅自转小写会让用户名带大写字母的人提不了权。

## 反向代理

```env
TRUST_PROXY=false
```

登录和注册接口按客户端 IP 限流。默认只信任 TCP 连接来源；
部署在 Nginx、Traefik、云负载均衡等反向代理后面时**必须**设为 `true`，
否则所有请求的来源 IP 都是代理地址，会被算作同一个客户端——
一个人触发限流就会导致所有用户都无法登录。

## 批量任务

```env
# 每个任务内部的并发数
JOB_WORKERS=8
# 同一任务里两个账号之间的间隔（毫秒），0 表示不等待
JOB_ACCOUNT_DELAY_MS=0
# job_events 的保留天数，超期的在启动时清理
JOB_EVENT_RETENTION_DAYS=7
```

默认值按「5000 个账号、单账号约 1.5 秒」估算：8 并发约 16 分钟跑完。
**调大并发时要同时调大 `JOB_ACCOUNT_DELAY_MS`**——批量刷新是最容易触发服务商风控的操作，
而风控的代价是用户的账号被封，比慢几分钟严重得多。
