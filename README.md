<div align="center">
  <img src="assets/logo.svg" width="128" height="128" alt="Emailbox 图标">
  <h1>Emailbox</h1>
  <p><strong>导入 · 托管 · 统一收信</strong></p>
  <p>
    <a href="https://github.com/MasterAlanLab/emailbox/releases"><img src="https://img.shields.io/github/v/release/MasterAlanLab/emailbox?display_name=tag&amp;style=flat-square&amp;logo=github&amp;logoColor=white&amp;label=Release&amp;color=E3A72F&amp;cacheSeconds=300" alt="最新版本"></a>
    <a href="https://github.com/MasterAlanLab/emailbox/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/MasterAlanLab/emailbox/ci.yml?branch=main&amp;style=flat-square&amp;logo=githubactions&amp;logoColor=white&amp;label=CI" alt="CI 状态"></a>
    <a href="https://github.com/users/MasterAlanLab/packages/container/package/emailbox"><img src="https://img.shields.io/badge/Docker-amd64%20%7C%20arm64-2496ED?style=flat-square&amp;logo=docker&amp;logoColor=white" alt="Docker: amd64, arm64"></a>
    <a href="https://github.com/MasterAlanLab/emailbox/releases"><img src="https://img.shields.io/badge/Desktop-macOS%20ARM64%20%7C%20Windows%20x64%20%7C%20Linux%20x64-17191D?style=flat-square" alt="桌面版: macOS ARM64, Windows x64, Linux x64"></a>
  </p>
</div>

面向批量邮箱账号管理的多租户服务。将 Outlook、Gmail、QQ、163 等第三方邮箱的账号、代理、令牌和收件箱集中到一个界面中，支持 Web、Docker 和桌面版。

脚本或 AI Agent 也可以通过只读 API Key 读取邮件和下载附件。

> Emailbox 会保管第三方邮箱凭据。公开部署前请配置 HTTPS、独立加密密钥和强管理员密码，并妥善备份密钥。

## 功能特性

- 批量导入：粘贴文本即可导入账号，自动识别三种常见格式；错误按行返回，其余账号继续处理。
- 统一收信：Microsoft Graph、IMAP 新版和旧版按回退链工作，可同时查询收件箱与垃圾箱。
- 令牌维护：支持全部刷新、失败账号刷新、分组刷新和定时刷新；失败原因区分过期、撤销、权限与配置问题。
- OAuth 重新授权：Outlook 账号可直接重新完成 Microsoft OAuth，Graph 与 IMAP OAuth 共用通道回退。
- 分组代理：按分组配置 SOCKS5 / HTTP 代理，支持 `{mail}` 模板和主备代理切换。
- 只读 API：为自动化脚本和 AI Agent 提供受限 API Key，并通过 `/llms.txt` 暴露接口说明。
- 用户隔离：每位用户的账号、邮件、分组、任务和用量独立保存，平台管理员的跨用户操作留有审计记录。
- 凭据保护：密码、令牌和代理口令加密存储；凭据导出强制审计并按用户限流。
- 邮件正文防护：正文经 DOMPurify 净化后在 sandbox iframe 中渲染，远程图片默认阻断。
- 双数据库：本地或轻量部署使用 SQLite，托管规模扩大后可切换 PostgreSQL。

## 安装

### 桌面版

从 [Releases](https://github.com/MasterAlanLab/emailbox/releases) 下载对应平台的发布包：

| 平台                  | 发布包                                  | 安装方式                                        |
| :-------------------- | :-------------------------------------- | :---------------------------------------------- |
| macOS (Apple Silicon) | `emailbox-<version>-macos-arm64.zip`    | 解压后将 `Emailbox.app` 拖入「应用程序」        |
| Windows (x64)         | `emailbox-<version>-windows-amd64.zip`  | 解压后运行 `emailbox.exe`                       |
| Linux (x64)           | `emailbox-<version>-linux-amd64.tar.gz` | 解压后运行 `./emailbox`，依赖见包内 README.txt |

桌面版在本机随机的 `127.0.0.1` 端口运行，通过系统 WebView 打开。首次启动会自动创建本地账号与加密密钥，无需手动登录，也不会占用 Web 版默认的 1323 端口。

需要注意：

- 发布包尚未签名。macOS 首次打开时需在「系统设置 → 隐私与安全性」中选择「仍要打开」；Windows SmartScreen 中选择「更多信息 → 仍要运行」。
- 仅提供 macOS Apple Silicon、Windows x64 和 Linux x64 三个目标；macOS Intel 与 Linux arm64 可使用 Docker 版。
- Linux 需要系统提供 `libwebkit2gtk-4.1`，各发行版的安装命令见包内 README.txt。
- 数据位于 macOS `~/Library/Application Support/emailbox/`、Windows `%AppData%\emailbox\` 或 Linux `~/.config/emailbox/`。
- `encryption.key` 是解密全部邮箱凭据的唯一密钥。迁移设备时应与 `app.db` 一起迁移，不要放入会被同步或共享的目录。

### Docker Compose

```bash
git clone https://github.com/MasterAlanLab/emailbox.git
cd emailbox
mkdir -p data
docker compose up -d --build
```

打开 <http://localhost:1323>。Linux 主机需要确保容器用户 uid 1000 对 `data` 目录有写入权限。

生产环境的加密密钥、HTTPS Cookie、反向代理和 PostgreSQL 配置见 [Docker 部署](docs/docker.md) 与 [配置说明](docs/configuration.md)。

### GHCR 镜像

当前版本为 `v0.3.3`：

```bash
docker pull ghcr.io/masteralanlab/emailbox:v0.3.3
mkdir -p data
docker run -d \
  --name emailbox \
  --restart unless-stopped \
  -p 1323:1323 \
  -v "$PWD/data:/app/data" \
  ghcr.io/masteralanlab/emailbox:v0.3.3
```

生产部署至少需要设置 `APP_ENV=production` 和 `ENCRYPTION_KEY`。

## API 与 OAuth

登录后，左侧「API」页面会显示 API Key、工作空间 ID、可调用接口和 Agent 接入说明。API Key 只开放分组、账号与邮件读取权限，不允许修改账号或导出凭据。

Microsoft OAuth 默认使用参考项目的应用配置。重新授权时，如果浏览器最终跳转到 `http://localhost:8080`，将地址栏中的完整 URL 粘贴回弹窗。生产环境的回调地址配置见 [配置说明](docs/configuration.md#microsoft-oauth-重新授权)。

## 构建

项目需要 Go 1.25+ 和 Bun 1.3.14：

```bash
git clone https://github.com/MasterAlanLab/emailbox.git
cd emailbox
cp .env.example .env
make deps

make dev       # 后端 :1323，前端 :5173
make build     # 构建包含前端资源的单二进制
```

开发模式打开 <http://localhost:5173>。管理员账号由 `.env` 中的 `BOOTSTRAP_ADMIN_USERNAME` 和 `BOOTSTRAP_ADMIN_PASSWORD` 创建，首次登录后请立即修改密码。

桌面版使用 Wails v3，只构建当前所在平台：

```bash
make package-desktop
```

安装包输出到 `dist-desktop/`。

## 测试

```bash
make lint
make test

# 修改 desktop/ 后额外运行
make lint-desktop
```

## 技术栈

- 后端：Go、Echo v5、`database/sql`、sqlc。
- 前端：React 19、TypeScript、Vite、Tailwind CSS v4、Cloudflare Kumo、Zustand。
- 数据库：SQLite / PostgreSQL。
- 邮件协议：Microsoft Graph、IMAP、OAuth 2.0。
- 桌面端：Wails v3 与系统 WebView。
- 部署：单二进制、Docker / GHCR、原生桌面包。

## 文档

- [配置说明](docs/configuration.md)：环境变量、加密密钥、管理员引导与 Microsoft OAuth。
- [Docker 部署](docs/docker.md)：GHCR 镜像、生产配置与已知限制。
- [开发方案](docs/plan/README.md)：架构、数据模型、协议层、API 和前端设计。
- [实施进度与踩过的坑](docs/plan/PROGRESS.md)：已完成工作和实现过程中的重要结论。
- [开发约定](AGENTS.md)：仓库结构、编码规则与测试口径。

## 联系

- YouTube：<https://www.youtube.com/@MasterAlanLab>
- Bilibili：<https://space.bilibili.com/3691004225914941>
- Telegram：<https://t.me/MasterAlanLab_Channel>
- 商务合作：<masteralanlab@gmail.com>

## 免责声明

- 本项目仅供学习交流与合法用途。请遵守所在地区的法律法规，并确认你对托管的每个邮箱账号拥有合法授权。
- 本项目处理第三方邮箱凭据。部署时请使用强密码与 HTTPS，并妥善备份数据库和加密密钥。

## 致谢

感谢 [outlookEmail](https://github.com/assast/outlookEmail) 提供协议与产品思路上的参考。
