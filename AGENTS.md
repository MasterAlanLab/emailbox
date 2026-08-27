# AGENTS.md

本文件是给在这个仓库里工作的人与 AI 代理看的约定。它只写**这个项目**的规则，
通用的 Go / React 教程内容一律不收——那些查官方文档更准。

## 1. 这是什么项目

Emailbox：面向公众注册的多租户 SaaS，批量托管第三方邮箱账号（Outlook OAuth / Gmail /
QQ / 163 等），提供统一收信、令牌刷新、代理配置与批量运维。

两件事决定了这个项目的绝大多数取舍：

1. **它保管的是别人的邮箱凭据。** 一次越权、一次不留痕的导出、一次误发到别的租户，
   后果都是直接泄露用户资产。因此隔离测试、审计、加密不是可选项。
2. **它是多租户的。** 任何一条 SQL 都必须带 `tenant_id`，任何一个端点都要能回答
   「A 租户的用户拿 B 租户的 ID 来请求会怎样」——答案必须是 404。

完整设计见 [`docs/plan/`](docs/plan/README.md)，进度与踩过的坑见
[`docs/plan/PROGRESS.md`](docs/plan/PROGRESS.md)。**动手前先看 PROGRESS 的「过程中发现的坑」**，
那里记的每一条都是已经付出过代价的。

## 2. 核心原则

- **只做必要的事**：不引入当前功能不需要的依赖和抽象。依赖每多一个，供应链面就大一圈。
- **代码自解释，注释解释「为什么」**：命名和结构说明「做了什么」，注释留给
  「为什么这么做」「不这么做会怎样」。本仓库的注释密度偏高，且几乎都是后者，请保持这个风格。
- **失败要响亮**：解密失败、配置非法、通道构造失败，一律报错而不是回落到某个"安全"默认值。
  静默降级在这个项目里等于「用户以为走了代理，其实从服务器公网 IP 直连」。
- **一致性优于个性**：跟着周围代码的写法走。

## 3. 技术栈

**后端**：Go 1.25+、Echo v5、`database/sql` + sqlc（无 ORM）、`log/slog`、
SQLite（modernc 纯 Go 驱动）/ PostgreSQL 双引擎、bcrypt、AES-256-GCM、
go-imap/v2（锁定 beta 版本，封装在 `pkg/mailer/imapx` 内不外泄类型）。

**前端**：TypeScript、React 19、Vite、Tailwind CSS v4、
[Cloudflare Kumo](https://kumo.cloudflare.com)（`@cloudflare/kumo`）、
`@phosphor-icons/react`、Zustand v5、React Router v7、Axios、Vitest、Bun。

不用的东西也写清楚，免得反复提议：没有 ORM、没有 GraphQL、没有表单库、没有 i18n
（文案直接写中文）、没有 shadcn（已随 Kumo 接入删除）、没有 toast 库
（错误以内联红字展示）。

## 4. 目录与分层

```text
api/            路由装配（mountMailRoutes 同时给用户侧与管理员侧用）+ 接口级测试
cmd/mailprobe/  协议层诊断 CLI
configs/        环境变量
db/migrations/  两套同版本号的迁移（sqlite / postgres）
db/query/       sqlc 命名 SQL，两个方言各一份
db/generated/   sqlc 生成代码，不手改
pkg/crypto/     AES-256-GCM 凭据加解密
pkg/quota/      配额计算与消耗
pkg/mailer/     协议层：graph/、imapx/、回退链、代理、导入导出格式
pkg/job/        任务系统：Manager、worker pool、事件广播
pkg/handler/    HTTP 处理器、审计中间件、SSE writer
pkg/service/    业务逻辑
pkg/repo/       数据访问，在此吸收两种方言的差异
pkg/middleware/ 会话、租户成员、平台管理员
pkg/model/      数据结构、DTO、RBAC 权限矩阵
web/src/        React 前端
```

| 层 | 职责 | 不该出现在这里的东西 |
|---|---|---|
| handler | 解析参数、调 service、映射错误到 HTTP | 业务规则、SQL |
| service | 业务规则、事务、加解密、配额、编排协议层 | `echo.Context`、HTTP 状态码 |
| repo | 调 sqlc 生成的查询，吸收方言差异 | 业务判断 |
| model | 结构体、常量、权限矩阵、`Normalize()` 这类纯函数 | 数据库与网络访问 |

文件超过 ~200 行就考虑拆分（`AccountService` 的导入、导出、批量各自成文件就是这么来的）。

## 5. 后端规约

### 5.1 统一响应格式

```jsonc
{ "code": 0, "data": {}, "message": "操作成功" }        // 成功
{ "code": 1, "data": null, "message": "具体错误信息" }  // 失败
{ "code": 0, "data": { "items": [], "pagination": { "page": 1, "limit": 50, "total": 0, "pages": 0 } } }
```

需要前端分支处理的业务码（定义在 `pkg/handler/response.go`）：

| code | 含义 |
|---|---|
| 1001 | 超出配额，`data` 带 `{metric, limit, used}` |
| 1003 | 用户已被管理员禁用 |
| 1004 | 邮箱账号已存在 |
| 1005 | 上游邮件服务失败，`data` 带 `{error_kind, channel}` |

`error_kind` 必须回传：前端据此区分「重新授权」「联系服务商」「检查代理」三种完全不同的处置，
只给一段文案用户不知道该做什么。

**401 只表示「本次请求的调用方没通过认证」，不表示别的。**
托管邮箱的凭据失效（`auth_failed` / `consent_required`）属于上游问题，走 502 + 1005，
具体原因由 `error_kind` 承载——`upstreamFailure` 曾经把它映射成 401，
结果是用户导入的一批账号里有一个 token 过期、点开就把**用户本人**踢回登录页。由
`api/mail_messages_test.go:TestUpstreamAuthFailureIsNotUnauthorized` 钉住。

5xx 的原始错误只记日志不回传（`failure()` 已处理），日志里带 `request_id`。

### 5.2 双引擎 SQL

**两个引擎各写各的 SQL**，不追求写法一致；repo 层方法签名统一。三条硬约束：

1. **`db/query/` 下的 SQL 必须全部 ASCII。** sqlc 遇到多字节字符会静默截断生成的 SQL 常量，
   运行时报 `incomplete input`，且爆炸点常在另一条查询上。`db/query/query_test.go` 拦这个。
2. **`ORDER BY` 无法参数化。** `sqlc.arg()` 会被原样留在 SQL 文本里，裸 `?` 被静默丢弃。
   每个「排序字段 × 方向」各写一条查询，由 service 按白名单分派；两个方言的 `.sql`
   由同一个脚本生成以防漂移。
3. **变长 IN 用 `sqlc.slice()`（SQLite）/ `= ANY($n::text[])`（PostgreSQL）。**
   `json_each()` 走不通——sqlc 的 SQLite 解析器不认识它。

改查询的流程：改 `db/query/` → `make sqlc-generate` → 补 `pkg/repo/parity_test.go` 的用例。
CI 会跑 `sqlc-verify` 和 PostgreSQL service container。

`TestGeneratedSQLHasNoLeftoverDirectives` 扫描生成产物里是否残留 `sqlc.arg` / `sqlc.slice`
——上面几个坑都属于「sqlc 静默留下非法 SQL」，必须在 CI 拦住。

### 5.3 多租户与权限

**一个租户空间只属于一个用户。**数据模型保留了完整的多租户结构（以后要做团队协作时
不用改表），但前端不展示这个概念：没有工作区切换、没有成员管理，后台也只有一份用户清单
（`AdminTenantsPage` 已删，配额与「进入其邮箱」并进了用户行）。
新增涉及租户的界面时，先想清楚它对用户意味着什么——多半应该表述成「用户」而不是「工作空间」。


- 每条业务 SQL 都带 `WHERE tenant_id = ?`，`tenantID` 来自 URL 且已由中间件校验成员身份
- 权限用 `middleware.Require(model.PermissionXxx)`，矩阵在 `pkg/model/permission.go`
- **平台管理员的跨租户放行只能有一处**：`middleware.Require` 里的那个口子，
  其前提是 `platform_user` 只由 `RequirePlatformAdmin` 设置。不要给管理员合成一个
  假的 `tenant_member`——那个假成员会流进审计和业务判断，事后分不清是真成员还是管理员
- 用户侧与管理员侧挂**同一份路由表**（`api.mountMailRoutes` 挂两次），不要抄第二遍

### 5.4 凭据与审计

- 邮箱密码、`refresh_token`、含口令的代理地址进出库经 `pkg/crypto`，密文永不出接口
  （`MailAccountResponse` 只回 `has_password` 这类布尔值）
- 代理地址要**先解密再打码**，解密失败回显「(无法解密)」而不是密文
- **协议层的写回必须是窄 UPDATE**（`auth_channel` / `refresh_token` / `last_refresh_*` 各一条），
  用整行改写会把用户此刻正在编辑的分组、备注、代理一起覆盖掉
- 写操作挂 `handler.AuditWrite`，管理员的读挂 `handler.AuditAdminRead`
  （只记管理员：普通用户翻十页邮件就是十条，会把真正要看的淹掉）
- **导出是全平台风险最高的接口**，四件必须同时在场：`account:secret` 权限、
  二次密码验证、强制审计、按用户限流。少任何一件都等于开了一个不设防或不留痕的凭据出口

### 5.5 配额

`pkg/quota` 的 `Effective`（`COALESCE(override, plan)`）、`CheckAndConsume`（先加后判、超额回滚）、
`CheckCount`。要点：

- 走远端**之前**扣（`daily_mail_fetch`），批量任务在**提交时**按数量一次性预扣
  （`daily_token_refresh`）——跑到一半再拒绝等于让用户为半批结果付全额
- 调低配额只挡新增、不追溯已有数据
- 批量导入超额的部分计 `skipped` 并说明原因，不要整批失败

### 5.6 并发与任务系统

- **协议层不许有全局状态**：每条连接自己的 `Dialer` / `http.Transport`。
  这是「批量拉信真并发」这个核心价值主张的前提，`TestConcurrentListsAreActuallyParallel` 守着它
- 任务状态入库（`jobs` / `job_items` / `job_events`），不放进程内存：批量刷新要跑几分钟，
  期间浏览器会断线、用户会刷新页面、服务可能重启
- 心跳 + 启动时 `ReapStale` 把僵尸任务标为 `interrupted`
- SSE 路径以 `/stream` 结尾（Gzip 中间件靠 `handler.IsSSEPath` 跳过，否则进度条不动），
  断线重连靠 `job_events.seq` 回放
- `repo.WithTx` 可重入：SQLite 生产配置只有一个连接，嵌套事务会静默挂死整个进程

### 5.7 日志

统一 `log/slog`，结构化字段而不是拼字符串。不记密码、token、完整凭据。
审计写失败只记 WARN 不影响业务——一个记不上日志的删除，好过一个因日志表满而拒绝服务的平台。

## 6. 前端规约

### 6.1 组件与目录

页面在 `src/pages/`，跨页面复用的在 `src/components/`（按域分子目录，如 `mail/`、`admin/`、`layout/`）。
组件文件 PascalCase，文件超过 ~200 行就拆。

**Kumo 没有的四样要自建**：Tree（`GroupTree`）、虚拟列表（`VirtualList`，
基于 `@tanstack/react-virtual`）、邮件正文渲染（`MessageBody`）、纵向分栏（`SplitPane`）。
自建组件跟随 Kumo 的 `forwardRef` / `displayName` / `cn()` 约定。

Kumo 这个版本**有** `DropdownMenu`（`@cloudflare/kumo/components/dropdown`），
`Trigger` / `Content` / `Item` / `Separator` / `Sub*` 一应俱全，`Item` 支持
`disabled` 与 `variant="danger"`。此处一度写着「没有」——那是更早的 Kumo 版本，
升级后已经可用，`GroupsPage` 的行内菜单就在用它。

> 用它写测试时注意：菜单渲染在 portal 里且外面套了几层 focus guard，
> `getAllByRole("menuitem")` 偶尔会漏掉第一项，直接 `querySelectorAll('[role="menuitem"]')`
> 更稳。禁用态读 `aria-disabled`，不是 `disabled` 属性（`Item` 渲染成 `div`）。

### 6.2 样式与主题

- **颜色一律走 Kumo 语义令牌**（`bg-kumo-base`、`text-kumo-subtle`、`border-kumo-line` …）。
  ESLint 拦截原始 Tailwind 色类与 `dark:` 前缀——Kumo 靠 `light-dark()` 自动切换，
  两套体系并存必然打架
- 容器用 `LayerCard`（`Surface` 已被 Kumo 标记 deprecated）。它的 `render` prop 可以
  把容器渲染成 `<form>`
- **站内跳转必须走 `LinkProvider` 注入的桥接组件**（`src/lib/AppLink.tsx`）：
  Kumo 的 `LinkButton` 默认渲染原生 `<a>`，直接用会整页刷新——本地点着「能跳转」，很容易漏掉
- **主题是显式开关**：Kumo 声明 `:root{color-scheme:light}` 与 `[data-mode="dark"]{color-scheme:dark}`，
  没人给根元素挂 `data-mode` 就永远是亮色。由 `src/lib/theme.ts` 在首屏渲染前挂好，
  默认跟随系统，顶栏按钮可手动切换并记住选择
- **换品牌观感改令牌本身，不在组件里覆盖类名**：`style.css` 里用 `@theme` 覆写
  `--color-kumo-brand` 等（Kumo 的 primary Button 在运行时读它并 `color-mix` 派生渐变）。
  Kumo 确实没有的另起 `--color-ebx-*` 命名空间，值一律写成 `light-dark()`。
  两处坑：① Kumo 在 `@layer base` 里还有一份 brand 定义，我们赢是因为 `@theme` 落在
  **无 layer** 的 `:root` 上（unlayered 优先于 layered），升级 Kumo 后要复查；
  ② Tailwind v4 会摇掉没被工具类引用的 `@theme` 变量，纯给 `var()`/JS 读的尺寸量
  要放普通 `:root`，否则产物里根本没有它
- **应用页一律用 `PageShell`**（`components/layout/PageShell.tsx`）：它统一了内容起始线、
  标题排版与间距。内容要更窄就在 children 里限宽，别改它的外层容器——
  改了标题会跟着挪，全站又会出现「切页面时标题横跳」的老问题
- **不要写 `calc(100vh - 顶栏 - 页脚)` 这类高度魔法数**：布局一改它就不再对应任何东西。
  需要占满剩余高度的，让父级成为 flex 容器、自己用 `flex-1`
- **视觉语言是 Linear**（[10 文档](docs/plan/10-linear-redesign.md)）。三条硬规则：
  **不写 `shadow-*`**（层次靠 surface 阶梯 + 1px hairline）、
  **display 标题配约 4% 的负字距**（用 `.display-xl/lg/md`，别自己拼 `text-*` + `tracking-*`）、
  **单色系统**（薰衣草只给品牌标记/主 CTA/focus ring，唯一彩色例外是状态点的 success）。
  按钮一律 8px 圆角，不做胶囊

### 6.3 Zustand

**选择器必须返回稳定引用。** zustand v5 走 `useSyncExternalStore`，
每次调用都新建对象/数组会让 React 认定「渲染期间状态一直在变」而无限重渲染
（生产构建里就是白屏 + `Minified React error #185`）：

```ts
// ✗ 每次都是新数组 —— 整页白屏
const ids = useSelectionStore((s) => Array.from(s.selected));
// ✗ 每次都是新对象 —— 同样白屏
const { user, loading } = useUserStore((s) => ({ user: s.user, loading: s.loading }));

// ✓ 取回原引用，需要别的形状就 useMemo
const selected = useSelectionStore((s) => s.selected);
const ids = useMemo(() => Array.from(selected), [selected]);
```

一个 store 一个功能域；更新用不可变写法。

### 6.4 数据获取

- 统一用 `src/lib/client.ts` 的 axios 实例（带 `withCredentials`）。错误体解析在拦截器里，
  它同时处理对象与 JSON 字符串——`responseType: "text"` 的接口（导出）不会自动 parse
- **effect 里取数要带 `ignore` 竞态守卫**：用户快速切换分组时，先发的慢请求后到会把新结果覆盖掉
- **不要在 effect 里同步 setState**（React 19 的 `react-hooks/set-state-in-effect` 会拦）。
  需要按 props 重置状态时用「带 `key` 的 state 对象 + 渲染期同步重置」：
  用 effect 重置会晚一帧，那一帧里旧数据挂在新标签下，此时点批量删除删的是看不见的东西
- 邮件类接口耗时以秒计（走上游 + 可能过代理），单独放大超时；超时被前端掐断最糟——
  配额已经在服务端扣了，用户什么都没拿到

### 6.5 邮件正文安全

DOMPurify 净化 + `sandbox` iframe 双层隔离（**不给** `allow-scripts`、**不给** `allow-same-origin`），
远程图片默认阻断。净化逻辑是纯函数（`messageDocument.ts`），改动必须补 XSS 回归用例。
两个已知陷阱记在 PROGRESS 里：DOMPurify 对 `img/video/audio/source/track` 的 `data:` 有捷径、
删 `<form>` 时默认保留子节点。

### 6.6 页面形态与响应式

**登录后没有顶栏**：左侧常驻导航栏（`AppSidebar`）+ 右侧内容区。形态由路由 `handle`
声明（`src/router/handle.ts`），`Layout` 分流，三种都由 `Layout.test.tsx` 钉住：

- 无 handle → 公开页（顶栏 + 文档流 + 页脚），只给还没登录的访客
- `appRoute` → 侧边栏 + 可滚内容区
- `shellRoute` → 侧边栏 + **不滚**内容区（`/mail`，滚动由内部面板各自负责）

移动优先，用 Tailwind 标准断点。`/mail` 的分栏按 [06 文档 §5.1](docs/plan/06-frontend.md)：
≥1280 三栏并列（右栏再纵向 split）、768~1280 左栏折进筛选栏的 `Select`、<768 单栏层级导航。
导航栏在 <768 强制收成 56px 图标条。

**断点判断优先用 CSS，不用 `matchMedia`**：JS 里再写一个 768 就多一个真源，
迟早和 Tailwind 的断点对不上。列宽这类取决于「容器有多宽」而不是「视口有多宽」的，
用 `@container`——账号列表夹在两栏中间，1440 视口下它自己只有 ~570px。

## 7. 测试口径

**只测核心行为与安全边界，不做防御性堆砌。** 一条用例要么钉住一个真会出错的判断，
要么守住一条安全边界；「每个分支都补一条」只会让测试比被测代码还长，
改一次实现要跟着改十处断言。新增用例优先并进已有文件，不为一个函数新开 `_test.go`。

不可省的两类：

1. **租户隔离**——A 租户用户访问 B 租户资源 → 404/403
2. **平台角色隔离**——普通用户访问 `/admin/*` → 403（`adminEndpoints` 表要与路由同步）

其余按层次：纯函数（格式识别、编码解码、配额计算）单测；协议层用 `httptest` 与
`imapmemserver` 进程内桩；repo 用跨引擎对照测试；service 测事务回滚与级联；
前端只测纯逻辑与安全相关的那几处。

**并发断言要做变异验证**：写完先人为制造一次串行/竞争，确认用例会红，再还原。
一条永远不会红的并发断言等于没写。

详见 [07 文档 §1](docs/plan/07-roadmap.md)。

## 8. 工具与命令

必需：Go 1.25+、Bun、sqlc、golangci-lint（版本见 [docs/golangci-lint.md](docs/golangci-lint.md)）、
Air（可选，热重载）。`make tools` 装齐，`make check` 检查。

```bash
make dev             # 后端 :1323 + 前端 :5173
make sqlc-generate   # 改完 db/query/ 后必跑
make sqlc-verify     # CI 也会跑
make lint            # golangci-lint + eslint + prettier 检查
make test            # go test + vitest
./scripts/build.sh   # 单二进制 + static/
```

提交前至少 `make lint && make test`。CI 会跑 `go test -race`、PostgreSQL 对照测试、
前端 lint / 格式检查与测试。

## 9. 改动之后

- 行为变了就改文档：`docs/plan/` 是设计依据，`PROGRESS.md` 记进度与坑，README 面向使用者
- 踩到「文档说的和现实不一样」的坑，**先改文档再改代码**，并把结论写进 PROGRESS 的坑列表——
  那份清单是这个项目里最有价值的部分
- 涉及用户可见文案时，中文、不加感叹号、说清楚「发生了什么」和「该怎么办」
