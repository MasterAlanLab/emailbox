# Emailbox · 批量邮箱托管平台

> 把成百上千个第三方邮箱（Outlook / Gmail / QQ / 163 …）集中托管起来：一次导入、统一收信、
> 令牌自动刷新、按分组走不同代理。网页里能点，也能交给一把只读 API Key，让脚本和 AI Agent 自己去取验证码。

<p>
  <img alt="单二进制" src="https://img.shields.io/badge/Go-单二进制·零依赖-00ADD8">
  <img alt="数据库" src="https://img.shields.io/badge/存储-SQLite%20%2F%20PostgreSQL-4479A1">
  <img alt="多租户" src="https://img.shields.io/badge/多租户-注册即隔离-brightgreen">
  <img alt="API" src="https://img.shields.io/badge/API-只读%20Key%20%2B%20llms.txt-orange">
</p>

### 📢 关注我的频道

分享有趣的技术原理、实用玩法和产品使用体验，欢迎关注：

[![YouTube](https://img.shields.io/badge/YouTube-FF0000?style=for-the-badge&logo=youtube&logoColor=white)](https://www.youtube.com/@MasterAlanLab)
[![Bilibili](https://img.shields.io/badge/Bilibili-00A1D6?style=for-the-badge&logo=bilibili&logoColor=white)](https://space.bilibili.com/3691004225914941)
[![Telegram](https://img.shields.io/badge/Telegram-0088CC?style=for-the-badge&logo=telegram&logoColor=white)](https://t.me/MasterAlanLab_Channel)

---

## 它解决什么问题

手里有一批邮箱账号的人，日常大概是这样的：要取一个验证码，先翻表格找账号，再登录，
登录发现令牌过期了，换个 IP 重试，收件箱里没有就去垃圾箱翻——一次几分钟，一天几十次。

Emailbox 把这件事变成：**账号一次性导入，之后在一个页面里找到它、点开、看到信**。
需要自动化时，把一把只读 Key 交给脚本或 AI Agent，让它自己去读。

**适用场景**

- 手里有一批注册用的邮箱，日常要频繁取验证码
- 想按客户 / 项目 / 批次分组管理，各走各的代理出口
- 想让脚本或 AI Agent 自动读取验证码，而不是自己一个个点
- 多个人共用一套部署，但各自的账号互相看不见

---

## 主要功能

- **批量导入**：粘贴一段文本就行，三种常见格式自动识别；哪一行有问题就报哪一行，不会整批失败
- **统一收信**：Microsoft Graph 与 IMAP 多通道自动回退，一条路不通自动换下一条，收件箱和垃圾箱一起查
- **令牌不再过期**：全部刷新、只刷失败的、或只刷某一个分组；进度实时可见，关掉页面再回来还能接着看
- **分组与代理**：按分组配 SOCKS5 / HTTP 代理，支持按邮箱名派生代理身份，主备三条自动切换
- **给 Agent 用的 API**：一个工作空间一把只读 Key，配套公开的 `/llms.txt`，AI Agent 读一遍就知道怎么调
- **各管各的**：注册即获得独立工作空间，账号、分组、用量互不可见
- **凭据安全**：密码与令牌加密落库，导出留审计；邮件正文在沙箱里渲染，远程图片默认拦截
- **部署简单**：单个二进制自带前端，SQLite 开箱即用，需要时换 PostgreSQL

---

## 快速开始

```bash
git clone <this-repo> && cd emailbox
cp .env.example .env      # 自带一个管理员：admin / admin123..
make deps && make dev     # 后端 :1323，前端 :5173
```

或者用 Docker：

```bash
docker compose up -d --build   # http://localhost:1323
```

打开页面 → 用 `admin` 登录 → **先去个人设置改掉默认密码** → 「导入」粘贴你的账号 → 开始收件。

> 想让脚本或 Agent 接入：左栏「API」页里有你的 Key、能调的接口，以及一段可以直接丢给 Agent 的接入说明。

---

## 文档

- [配置说明](docs/configuration.md) — 全部环境变量，含加密密钥与管理员引导
- [Docker 部署](docs/docker.md) — 镜像构建、生产配置与已知限制
- [开发方案](docs/plan/README.md) · [实施进度与踩过的坑](docs/plan/PROGRESS.md) — 架构、数据模型、协议层、API、前端
- [AGENTS.md](AGENTS.md) — 在这个仓库里写代码的约定

---

## 资源推荐

以下列表中部分链接为推广 / 推荐（affiliate）链接，通过它们注册或购买可能为作者带来少量返佣，
**不会额外增加你的花费**。

- **代理**：[Free Proxy](https://github.com/MasterAlanLab/free-proxy) — 我的另一个开源项目，在自己的 VPS 上跑一个免费代理池，正好接到本项目的分组代理里
- **海外账号、电话卡**：[点这里](https://cutt.ly/dywt86NC) — TG、TikTok 等海外平台账号
- **打码平台**：[Captcha.run](https://captcha.run/sso?inviter=542f4f4f-31b6-4b70-b485-c4762c45d1e8)（强烈推荐） · [YesCaptcha](https://cutt.ly/Mywt39r0)（便宜好用）
- **指纹浏览器**：[比特指纹浏览器](https://client.bitbrowser.cn/register?lang=zh&code=Alan123) — 日常在用，没什么硬伤
- **海外 VPS**：[搬瓦工](https://cutt.ly/qywJNWzd) · [DMIT](https://cutt.ly/YywJIzY0) — 三网优化线路，支持支付宝
- **海外虚拟信用卡**：[点这里](https://cutt.ly/IyrMR4Mg) — 付海外服务用得上
- **Telegram 资源搜索机器人**：[点这里](https://cutt.ly/2yeh3GOE) — TG 最强搜索引擎，试试看
- **GPT 中转站**：[满血 CC / GPT 中转](https://cutt.ly/JywJG3G5) — 确认不掺水，缺点是价格偏高
- **订阅合租拼车**：[点这里](https://cutt.ly/5ywt8vb4) — 影视会员、AI 订阅都能合租

---

## 💬 交流与反馈

- TG 频道：<https://t.me/MasterAlanLab>
- 商务合作：masteralanlab@gmail.com

如果对你有帮助，记得点个 ⭐ 支持一下！

---

## 📄 免责声明

- 本项目仅供学习交流与**合法用途**，请遵守你所在地区的法律法规。你必须对托管的每个邮箱账号拥有合法授权。
- 本项目保管的是**第三方邮箱凭据**，敏感度高于自用工具：请自行设置强密码、开启 HTTPS，并妥善备份加密密钥。

## 🙏 致谢

[outlookEmail](https://github.com/assast/outlookEmail)
