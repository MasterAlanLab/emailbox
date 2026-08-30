# Emailbox · 批量邮箱托管与收信

把账号、代理、令牌和收件箱放到一个界面里。Emailbox 适合需要管理大量 Outlook、Gmail、QQ、163 等第三方邮箱的个人和小团队。

<p>
  <img alt="Go 单二进制" src="https://img.shields.io/badge/Go-单二进制·零依赖-00ADD8">
  <img alt="数据库" src="https://img.shields.io/badge/存储-SQLite%20%2F%20PostgreSQL-4479A1">
  <img alt="多租户隔离" src="https://img.shields.io/badge/多租户-注册即隔离-brightgreen">
  <img alt="GitHub Container Registry" src="https://img.shields.io/badge/GHCR-镜像-2496ED?logo=docker&logoColor=white">
</p>

我会在频道里分享技术原理、使用经验和产品更新：

[![YouTube](https://img.shields.io/badge/YouTube-FF0000?style=for-the-badge&logo=youtube&logoColor=white)](https://www.youtube.com/@MasterAlanLab)
[![Bilibili](https://img.shields.io/badge/Bilibili-00A1D6?style=for-the-badge&logo=bilibili&logoColor=white)](https://space.bilibili.com/3691004225914941)
[![Telegram](https://img.shields.io/badge/Telegram-0088CC?style=for-the-badge&logo=telegram&logoColor=white)](https://t.me/MasterAlanLab_Channel)

## 为什么做它

邮箱账号一多，流程很快变成这样：从表格里找账号，登录后发现令牌过期，换代理再试；收件箱没有验证码，还要去垃圾箱翻一遍。每天重复几十次，时间都花在查找和重试上。

Emailbox 把这些步骤集中起来：账号批量导入，邮件统一查看，Graph 和 IMAP 自动回退，认证或权限异常时从 Token 页查看具体原因。脚本或 AI Agent 也可以通过只读 API Key 读取邮件。

## 主要功能

- **批量导入**：粘贴文本即可，自动识别三种常见格式；错误按行返回，其余账号继续处理
- **统一收信**：Microsoft Graph、IMAP 新版和旧版按回退链工作；收件箱与垃圾箱可以一起查询
- **令牌维护**：支持全部刷新、按失败账号刷新和按分组刷新；Graph 与 IMAP OAuth 共用通道回退，失败原因区分过期、撤销、权限与配置问题；Outlook 账号可直接重新走 Microsoft OAuth
- **分组代理**：按分组配置 SOCKS5 / HTTP 代理，支持 `{mail}` 模板和主备代理切换
- **只读 API**：每个工作空间一把 API Key，附带公开的 `/llms.txt`，方便脚本和 Agent 接入
- **租户隔离**：注册后自动获得独立工作空间，账号、邮件、分组和用量互相隔离
- **凭据保护**：密码和令牌加密存储，导出操作写入审计日志；邮件正文使用沙箱 iframe 渲染，远程图片默认阻断
- **两种数据库**：本地使用 SQLite，规模扩大后切换 PostgreSQL

## 运行

### 从源码启动

```bash
git clone https://github.com/MasterAlanLab/emailbox.git
cd emailbox
cp .env.example .env
make deps && make dev
```

打开 <http://localhost:5173>。管理员账号由 `.env` 中的 `BOOTSTRAP_ADMIN_USERNAME` 和
`BOOTSTRAP_ADMIN_PASSWORD` 创建；本地首次登录后请立即修改密码。

### Docker Compose

```bash
git clone https://github.com/MasterAlanLab/emailbox.git
cd emailbox
docker compose up -d --build
```

打开 <http://localhost:1323>，导入账号即可开始收信。生产环境的加密密钥、HTTPS Cookie、反向代理和 PostgreSQL 配置见 [Docker 部署](docs/docker.md) 与 [配置说明](docs/configuration.md)。

### 使用 GHCR 镜像

当前版本是 `v0.2.1`，镜像位于 [GitHub Container Registry](https://github.com/users/MasterAlanLab/packages/container/package/emailbox)：

```bash
docker pull ghcr.io/masteralanlab/emailbox:v0.2.1
mkdir -p data
docker run -d \
  --name emailbox \
  --restart unless-stopped \
  -p 1323:1323 \
  -v "$PWD/data:/app/data" \
  ghcr.io/masteralanlab/emailbox:v0.2.1
```

生产部署请配置 `APP_ENV=production` 和 `ENCRYPTION_KEY`。Linux 主机首次挂载 `data` 目录时，需要让容器的 uid 1000 具备写入权限。

## API 和 OAuth

登录后，左侧「API」页面会显示 API Key、可调用接口和 Agent 接入说明。

Microsoft OAuth 默认使用参考项目的应用配置。重新授权时，如果浏览器最终跳到
`http://localhost:8080`，将地址栏里的完整 URL 粘贴回弹窗；生产环境的回调地址配置见
[配置说明](docs/configuration.md#microsoft-oauth-重新授权)。

## 文档

- [配置说明](docs/configuration.md) — 环境变量、加密密钥、管理员引导与 Microsoft OAuth
- [Docker 部署](docs/docker.md) — GHCR 镜像、生产配置与已知限制
- [开发方案](docs/plan/README.md) — 架构、数据模型、协议层、API 和前端
- [实施进度与踩过的坑](docs/plan/PROGRESS.md) — 已完成工作和实现过程中的重要结论
- [AGENTS.md](AGENTS.md) — 仓库开发约定

## 资源推荐

下面列出一些我自己使用过、或认为适合这套工作流的服务。部分链接属于推广 / 推荐（affiliate）链接；通过它们注册或购买可能为作者带来少量返佣，**不会额外增加你的花费**。

- **代理**：[Free Proxy](https://github.com/MasterAlanLab/free-proxy) — 在自己的 VPS 上运行免费代理池，接入 Emailbox 的分组代理
- **海外账号、电话卡**：[点这里](https://cutt.ly/dywt86NC) — TG、TikTok 等海外平台账号
- **打码平台**：[Captcha.run](https://captcha.run/sso?inviter=542f4f4f-31b6-4b70-b485-c4762c45d1e8) · [YesCaptcha](https://cutt.ly/Mywt39r0)
- **指纹浏览器**：[比特指纹浏览器](https://client.bitbrowser.cn/register?lang=zh&code=Alan123)
- **海外 VPS**：[搬瓦工](https://cutt.ly/qywJNWzd) · [DMIT](https://cutt.ly/YywJIzY0)
- **海外虚拟信用卡**：[点这里](https://cutt.ly/IyrMR4Mg)
- **Telegram 资源搜索机器人**：[点这里](https://cutt.ly/2yeh3GOE)
- **GPT 中转站**：[满血 CC / GPT 中转](https://cutt.ly/JywJG3G5)
- **订阅合租拼车**：[点这里](https://cutt.ly/5ywt8vb4)

## 联系我

- Telegram 频道：<https://t.me/MasterAlanLab>
- 商务合作：<mailto:masteralanlab@gmail.com>

如果 Emailbox 对你有用，欢迎点个 ⭐。反馈和 issue 也很有帮助。

## 免责声明

- 本项目仅供学习交流与**合法用途**。请遵守所在地区的法律法规，并确认你对托管的每个邮箱账号拥有合法授权。
- 本项目处理的是**第三方邮箱凭据**。部署时请设置强密码、启用 HTTPS，并妥善备份加密密钥。

## 致谢

感谢 [outlookEmail](https://github.com/assast/outlookEmail) 提供协议与产品思路上的参考。
