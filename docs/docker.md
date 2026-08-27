# Docker 部署

镜像是多阶段构建：第一阶段用 bun 构建前端，第二阶段编译 Go 并把前端产物复制到
`static/`，最终镜像只包含一个静态链接的二进制和静态文件。

因为使用纯 Go 的 `modernc.org/sqlite`，构建时 `CGO_ENABLED=0`，不需要 gcc。

## 本地运行

默认使用 SQLite：

```bash
docker compose up --build
```

数据库文件通过 `./data` 挂载持久化到宿主机。

启用附带的 PostgreSQL：

```bash
docker compose --profile postgres up -d postgres
```

然后给应用设置 `DB_DRIVER=postgres` 和对应的 `DB_*` 变量。
迁移会在应用启动时按当前数据库方言自动执行，无需手动操作。

## 只构建镜像

```bash
make docker              # 等价于 docker build -t emailbox .
```

不需要先跑 `make build`：Dockerfile 自带完整构建阶段，而 `.dockerignore`
会把本地的 `web/dist`、`static`、`server` 排除在构建上下文之外。

## 部署到生产环境

`docker-compose.yml` 里的配置面向本地开发，直接用于生产至少需要调整这几处：

```yaml
environment:
  # 生产模式：缺 ENCRYPTION_KEY 时由「启动告警」升级为「启动失败」
  APP_ENV: "production"
  # 邮箱凭据的加密密钥（make gen-key 生成）。它必须与数据卷一起备份，
  # 且不能每次部署重新生成——密钥变了，已存的凭据就全部解不开了
  ENCRYPTION_KEY: "..."
  # HTTPS 部署必须为 true，否则会话 Cookie 可能被明文传输
  COOKIE_SECURE: "true"
  # 允许跨域的前端来源，不接受 *，配置非法会在启动时报错退出
  CORS_ALLOW_ORIGINS: "https://app.example.com"
  # 位于 Nginx / 云负载均衡之后时必须为 true，
  # 否则限流会把所有用户当成同一个 IP，一个人触发就会导致全站无法登录
  TRUST_PROXY: "true"
```

完整配置项见 [`configuration.md`](configuration.md)。

## 已知限制

默认的 SQLite 只有一个写连接，批量导入与批量刷新会串行；托管量上来之后
用附带的 PostgreSQL profile（见上）。

以下几点尚未处理，正式部署前建议自行加固：

- **容器以 root 运行**，且挂载了宿主机的 `./data`，写入的文件属主是 root。
  如需非 root，在 Dockerfile 中添加用户并调整 `/app/data` 属主。
- **运行阶段基于 `alpine:latest`**，未固定版本，不同时间构建同一个 commit
  可能得到不同的运行时。生产环境建议固定到具体版本。
- **镜像内没有时区数据**，`time.LoadLocation` 在容器里会失败。需要时区支持时
  安装 `tzdata` 或在代码中导入 `time/tzdata`。
- **没有健康检查**。原先探测 `/api/v1/health` 的那个已随接口一起删除：它返回静态 JSON，
  不碰数据库也不看静态文件，前端资源缺失或数据库不可用时照样 200——除了「进程还在」
  什么都测不出，而进程没了本来也不需要它来发现。要加回健康检查的话，
  应探测一个真的会因依赖故障而失败的端点，不要再放静态探针。
- **Go 模块代理被固定为 `goproxy.cn`**（Dockerfile 中），在中国大陆以外构建
  可能很慢或不可达，可通过修改该行或改用构建参数覆盖。
