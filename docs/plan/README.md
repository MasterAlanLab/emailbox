# emailbox 批量邮箱管理平台 · 开发方案

本目录是把当前 `emailbox`（Go + React 模板）扩展为**多租户批量邮箱管理平台**的完整开发方案。
方案的功能与协议依据来自同仓库内的 Python 项目 `outlookEmail/`（Flask + SQLite，约 2.5 万行），
但**不是移植代码**，而是把它验证过的协议链路、失败回退策略和数据模型，用当前模板的
Go 分层架构 + sqlc 双驱动 + React 前端重新实现。

## 文档索引

| 文档 | 内容 |
|------|------|
| [01-analysis.md](01-analysis.md) | 两个项目的现状分析、可借鉴点与不可照搬点 |
| [02-architecture.md](02-architecture.md) | 目标架构、包划分、并发模型、配置与密钥管理 |
| [03-data-model.md](03-data-model.md) | 完整数据库设计、迁移与 sqlc 组织方式 |
| [04-mail-protocol.md](04-mail-protocol.md) | 邮件协议层：Graph / IMAP / OAuth / 代理 / 回退链 |
| [05-api-design.md](05-api-design.md) | 后端 API 设计 |
| [06-frontend.md](06-frontend.md) | 前端信息架构、页面、状态管理与批量交互 |
| [07-roadmap.md](07-roadmap.md) | 分阶段实施计划、验收标准、测试与风险 |
| [08-saas-admin.md](08-saas-admin.md) | SaaS 形态：个人工作空间、平台管理员、配额、用户管理 |
| [PROGRESS.md](PROGRESS.md) | **实施进度**：各阶段完成状态，以及实现过程中踩到的坑 |

## 当前状态

P0 地基 / P1 分组与账号 / P2 邮件协议层 / P3 管理后台 / P4 任务系统与 Token 刷新 已完成，
**P4 之后就是全部范围**。原先规划的 P5 转发与调度、P6 对外能力、P7 增强已于 2026-08-21
从方案中删除，相关设计不再保留在本目录里（清单见 [07-roadmap.md §5](07-roadmap.md)）。

本目录写的是**设计目标**。实现过程中被现实修正过的地方（sqlc 的若干限制、
04 文档 IMAP 文件夹表里的一处错值、路由级 BodyLimit 的行为等）以
[PROGRESS.md](PROGRESS.md) 的「过程中发现的坑」为准——那里的每一条都已同步回对应的设计文档。

## 一句话目标

做成一个**面向公众注册的 SaaS**：普通用户注册即获得独立工作空间，托管自己的邮箱且与他人完全隔离；
平台管理员可管理用户、并跨工作空间查看与操作全系统邮箱。

在保留模板既有的「用户 / 租户 / 成员 / 会话 / RBAC」体系之上，新增一条完整的邮箱业务线：
**分组树 → 邮箱账号（批量导入）→ 邮件读取（Graph/IMAP 多通道回退）→ 批量运维（Token 刷新、代理、分组调整）**。

## 三个已定的关键决策

| 决策 | 选择 | 依据 |
|---|---|---|
| 隔离单位 | 保留 `tenants`，注册自动创建 `kind='personal'` 的个人工作空间 | 复用模板全部租户中间件与权限代码；将来做团队版无需数据迁移。UI 上对普通用户隐藏 |
| 前端组件库 | Cloudflare **Kumo**（`@cloudflare/kumo`） | React + Base UI + Tailwind v4，与模板技术栈天然匹配；页面实现优先用现成组件，仅 Tree / 虚拟列表 / 邮件正文三处自建 |
| 双引擎 SQL | **两个引擎各写各的 SQL**，不追求写法一致 | SQLite 用 `json_each()`，PostgreSQL 用 `= ANY()`；repo 层方法签名统一，靠跨引擎对照测试防漂移 |

配额体系：**做配额、不做计费**（`plans` + `tenant_quotas` + `usage_counters`），计费不做。

## 与 outlookEmail 的关系

| 维度 | outlookEmail | emailbox（目标） |
|------|--------------|------------------|
| 使用者模型 | 单用户，一个登录密码 | 公开注册 SaaS：个人工作空间隔离 + 平台管理员 |
| 前端 | 原生 JS + Jinja 模板 | React 19 + TS + **Cloudflare Kumo** + Tailwind v4 + Zustand |
| 资源限制 | 无 | 套餐 + 配额（账号数、分组数、每日拉信/刷新次数） |
| 后端 | Flask 单进程单 worker | Go + Echo v5，单实例部署 |
| 数据库 | SQLite，运行时 `ALTER TABLE` 自迁移 | SQLite / PostgreSQL 双驱动，版本化迁移 + sqlc |
| 敏感数据 | Fernet + 固定盐 PBKDF2 | AES-256-GCM + 每记录随机 nonce + 密钥版本 |
| IMAP 代理 | 全局 monkeypatch `socket.socket` + 进程锁（串行瓶颈） | 每连接自定义 `Dialer`，天然并发 |
| 批量能力 | 串行 / 少量线程，SSE 进度 | worker pool + 任务表 + SSE，可断点续看 |

## 阅读顺序建议

先看 [01-analysis.md](01-analysis.md) 理解「借鉴什么」，再看 [04-mail-protocol.md](04-mail-protocol.md)
——那是整个平台唯一无法靠常识推导、必须照抄 outlookEmail 实战经验的部分。
其余文档按需查阅，实施顺序见 [07-roadmap.md](07-roadmap.md)。
