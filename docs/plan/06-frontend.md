# 06 · 前端设计（Cloudflare Kumo）

## 1. 组件库：`@cloudflare/kumo`

前端统一使用 Cloudflare 的 Kumo 组件库。它基于 **React + Base UI + Tailwind CSS v4**，
自带语义化设计令牌与自动亮/暗模式。模板已经是 Tailwind 4 + React 19，**版本天然匹配**。

### 1.1 安装

```bash
cd web && bun add @cloudflare/kumo @phosphor-icons/react
```

| 依赖 | 版本要求 | 说明 |
|---|---|---|
| `@cloudflare/kumo` | latest | ESM only |
| `react` / `react-dom` | 18 或 19 | 模板已是 19 ✓ |
| `@phosphor-icons/react` | ^2.1.10 | Kumo 的图标依赖（peer） |
| `zod` | ^4（可选） | 表单校验，建议引入用于导入向导与设置页 |

### 1.2 CSS 接入（顺序不能错）

`web/src/style.css` 的**开头**改成：

```css
@source "../node_modules/@cloudflare/kumo/dist/**/*.{js,jsx,ts,tsx}";
@import "@cloudflare/kumo/styles/tailwind";
@import "tailwindcss";
```

当前 `style.css` 第一行是 `@import "tailwindcss";`，**必须把 Kumo 的两行放到它前面**，
否则 Kumo 的令牌层会被 Tailwind 的 preflight 覆盖，表现为组件样式部分失效。

### 1.3 需要清理的模板遗留

| 项 | 处理 |
|---|---|
| `style.css` 里的 `@theme inline { --color-background: #ffffff; ... }` 整块自定义令牌 | **删除**。这是 shadcn 风格的令牌，与 Kumo 的 `kumo-*` 令牌并存会导致两套颜色体系打架 |
| `style.css` 的 `@layer base` 里写死的 `#ffffff` / `#18181b` | **删除**。Kumo 靠 `light-dark()` 自动切换，写死颜色会锁死亮色模式 |
| `web/components.json`（shadcn 配置） | **删除**。不再使用 shadcn |
| `lucide-react` 依赖 + `DashboardPage.tsx` / `HomePage.tsx` 里的用法 | 改为 `@phosphor-icons/react`，然后从 `package.json` 移除 lucide（避免两套图标 = 两份体积） |
| 现有页面里的 `bg-white` / `text-zinc-*` 等原始色类 | 逐页替换为语义令牌（见 §1.4） |

### 1.4 主题规则（Kumo 强约束，必须遵守）

Kumo 的 lint 规则明确禁止若干写法，团队约定照单执行：

- **只用语义令牌**：`bg-kumo-base`、`bg-kumo-elevated`、`bg-kumo-recessed`、
  `text-kumo-default`、`border-kumo-line`、`ring-kumo-hairline`
- **禁止原始 Tailwind 颜色**（`bg-blue-500`、`text-zinc-600` …）
- **禁止 `dark:` 变体**——暗色模式由 CSS 自定义属性自动处理，写 `dark:` 反而会破坏它
- 例外白名单：`bg-white`、`bg-black`、`text-white`、`text-black`、`transparent`
- 表面层次递进：`bg-kumo-base`（页面）→ `bg-kumo-elevated`（卡片/浮层）→ `bg-kumo-recessed`（凹陷区/代码块）
- **不覆盖 `--color-kumo-*`**：Kumo 就是 Cloudflare 自己的设计系统，它的默认值即是
  Cloudflare 的配色——纯灰中性色（chroma 为 0），蓝只做链接文字，橙 `#f6821f` 只做品牌标记。
  抄一份到 `style.css` 只会随 Kumo 升级漂移。那里只剩 `--color-ebx-*`：
  状态点、分组色、品牌标识的橙色渐变，都是 Kumo 没有的
- **全站按钮只有一种长相**：Kumo 的 `Button` / `LinkButton` + `variant="secondary"`
  （白底 + hairline），危险动作 `variant="secondary-destructive"`（白底红字）。
  不用 `primary` / `destructive` 那两个实心色块变体：一排平级动作里单给某一个填色
  是替用户做选择，蓝白相邻的高对比也刺眼。强调靠 `size="lg"` 和留白
- className 组合用 Kumo 的 `cn()` 工具（模板 `lib/utils.ts` 已有同名函数，二选一，建议统一用 Kumo 的）
- 暗色切换：在根节点切 `data-mode="dark"`，不用 class 策略。
  **这一步是必须的，不是可选的**：Kumo 只声明 `:root{color-scheme:light}` 与
  `[data-mode="dark"]{color-scheme:dark}`，而所有颜色靠 `light-dark()` 解析——
  没人挂 `data-mode` 的话，语义令牌写得再规范也永远是亮色。
  实现在 `web/src/lib/theme.ts`：首屏渲染前挂好（否则会闪一帧亮色）、
  默认跟随系统 `prefers-color-scheme`、顶栏按钮可手动切换并记住选择

> 建议在 `web/eslint.config.js` 里加一条自定义规则或 `no-restricted-syntax`，
> 拦截原始色类与 `dark:` 前缀。靠人工 review 守不住这条约定。

**对数据模型的影响**：[03 文档](03-data-model.md) 里 `mail_groups.color` 原本存 `#3b82f6` 这类十六进制值，
与「只用语义令牌」冲突。改为存**受限的令牌名枚举**（`blue` / `green` / `amber` / `red` / `purple` / `gray`），
由该列的 CHECK 约束固定。

> 但**不要**假设每个令牌名都有对应的 `bg-kumo-*` 工具类：Kumo 的 purple 只有文字色，
> `bg-kumo-purple` 不会被 Tailwind 生成，圆点会是透明的。分组配色因此自带一套
> `--color-ebx-group-*`（`style.css`），六个色一起定义。

## 2. Kumo 组件映射

Kumo 提供约 30 个组件。下表是本项目每个界面元素的落位，**优先用现成组件，不自己造**：

| 场景 | Kumo 组件 |
|---|---|
| 顶部导航 | `MenuBar` |
| 页面内层级导航 | `Breadcrumbs` |
| 全局搜索邮箱（`Cmd+K`） | `CommandPalette` ★ 与「按邮箱地址全局检索」需求高度契合 |
| 按钮 / 图标按钮 | `Button` |
| 复制邮箱 + 别名 | `ClipboardText` ★ 正好对应批量菜单里的「复制」 |
| 批量操作菜单、行内更多操作 | `DropdownMenu` |
| 表单输入 | `Input`、`Select`、`Checkbox`、`Radio`、`Switch`、`DatePicker` |
| 分组选择（可搜索） | `Combobox` |
| 邮箱地址输入联想 | `Autocomplete` |
| 状态标记（active/banned/failed） | `Badge` |
| 表格（用户管理、日志） | `Table` + `Pagination` |
| 文件夹切换、设置分页 | `Tabs` |
| 正文/说明文字 | `Text` |
| 配额用量、任务进度 | `Meter` ★ |
| 导入格式示例、任务日志、原始 MIME | `Code` |
| 卡片容器（Dashboard 统计、邮件详情） | `LayerCard` |
| 弹窗（导入向导、编辑账号、二次验证） | `Dialog` |
| 悬浮面板（代理配置提示、账号速览） | `Popover` |
| 提示气泡 | `Tooltip` |
| 页面级提示（配额告警、同步失败、免责声明） | `Banner` |
| 操作结果通知 | `Toast` |
| 加载中 | `Loader`、`SkeletonLine` |
| Dashboard 图表 | `Chart`、`TimeseriesChart` ★ 刷新成功率趋势、每日拉信量 |

### 2.1 Kumo 没有、需要自建的组件

| 需求 | 方案 |
|---|---|
| **虚拟滚动列表** | Kumo `Table` 面向常规数据量，不含虚拟化。账号列表/邮件列表另建 `VirtualList`（`@tanstack/react-virtual`），行内元素仍用 Kumo 的 `Checkbox`/`Badge`/`Text`/`Button` |
| **可拖拽分隔的布局** | 自建 `SplitPane`，用 CSS grid + 拖拽把手，尺寸存 localStorage（2026-08 改版后用在右栏的纵向切分上） |
| **邮件正文渲染** | 自建 `MessageBody`（sandbox iframe + DOMPurify，见 §6） |
| **右键上下文菜单** | Kumo 有 `DropdownMenu` 但无 ContextMenu 触发器。用 `onContextMenu` 手动定位一个 `Popover` |

新增前端依赖因此为：`@tanstack/react-virtual`、`dompurify`、`date-fns`（时间格式化）。

原本这里还有一行「三级分组树：无 `Tree` 组件，自建 `GroupTree`」。分组 2026-08-27 压平成
一层之后不需要树了：左栏的 `GroupList` 和管理页的列表都是平铺的行，直接复用 `SidebarRow`
与 `DropdownMenu`。

### 2.2 关于 Table vs 虚拟列表的分工

不要试图用一套组件覆盖全部列表，两种场景诉求不同：

- **用户管理 / 各类日志** → Kumo `Table` + `Pagination`。
  运维场景需要精确翻页、排序、列对齐，数据量每页 ≤100，无需虚拟化。
- **`/mail` 工作台的账号列表与邮件列表** → 自建 `VirtualList` + 无限滚动。
  这里追求的是「一直往下滚」的浏览体验，且单分组可能上万账号。

## 3. 信息架构

```
/                         首页（保留，改造为 SaaS 落地页）
/login /register          （保留，Kumo 化）
/mail                     ★核心工作台（三栏，右栏纵向再切）
/mail/import              批量导入向导
/mail/tokens              Token 刷新（范围选择 + 任务流）
/settings/profile         （保留）
/settings/security        （保留）
/settings/usage           配额与用量（Meter）
/settings/api             对外取件 API：Key + 接口清单 + llms.txt 入口
/resources                登录后可见的资源推荐
/admin                    ★管理员总览（仅 platform_role=admin 可见）
/admin/users              用户管理
/admin/tenants/:id/mail   以管理员身份查看/操作某租户的邮箱（复用 /mail 的组件）
/admin/plans              套餐管理
/admin/audit              全平台审计日志
```

模板已有的 `/tenant/settings`、`/tenant/members` 路由**保留但隐藏入口**
（个人工作空间用不到，团队版启用时直接复用）。

与最初设计的两处出入：`/dashboard` 概览页没有做——它要展示的东西分散在 `/mail` 顶栏、
状态栏与 `/settings/usage` 里，再来一页只是重复；`/settings/workspace` 落地成了
`/settings/usage`，因为「工作空间」这个概念对普通用户不露面（AGENTS.md §5.3）。

### 3.1 路由守卫

在现有 `ProtectedRoute` / `PublicRoute` 之外新增：

```tsx
// web/src/router/RouteGuards.tsx
export function AdminRoute({ children }: { children: React.ReactNode }) {
  const { user, loading } = useAuthStore();
  if (loading) return <Loader />;
  if (user?.platform_role !== "admin") return <Navigate to="/dashboard" replace />;
  return <>{children}</>;
}
```

**前端守卫只是体验优化，不是安全边界**——后端 `RequirePlatformAdmin` 中间件才是。
两者都必须有。

## 4. 状态管理

沿用 Zustand（AGENTS.md §6.3 有本项目的选择器约束，违反会白屏）。原计划为每个数据域
各开一个 store，实际只落地了四个：

```
web/src/store/
  authStore.ts          当前用户（含 platform_role）与会话恢复
  tenantStore.ts        当前工作空间
  selectionStore.ts     ★批量选择（账号 / 邮件 两套独立实例）
  jobStore.ts           任务列表 + SSE 连接状态与进度
```

**`mailGroupStore` / `mailAccountStore` / `mailMessageStore` / `quotaStore` / `adminStore`
都没有建**：它们的数据只有一个页面用，取回来就渲染，放进全局 store 只是多一份要手动
失效的副本。判断标准是「有没有第二个消费者」——分组列表同时被左栏、筛选栏、导入页用，
但它们都在 `/mail` 这一棵组件树里，props 传下去比全局 store 更容易看清数据从哪来。

**关于 react-query 的取舍**：邮件数据有「远端慢、需乐观更新、需后台重验证」的特征，
react-query 会简化不少。但模板既有代码全是 Zustand + `useAsyncAction`，混用会分裂风格。
**建议先不引入**；若 P2 发现邮件缓存/失效逻辑在 store 里明显失控，再作为独立决策评估。

### 4.1 `selectionStore`

批量选择是本平台核心交互，需求对齐 outlookEmail README §4：

```ts
interface SelectionState {
  mode: "idle" | "batch";          // 批量模式下点击整行即切换选中
  selected: Set<string>;
  anchorId: string | null;         // Shift 范围选择锚点
  toggle(id: string): void;
  selectRange(fromId: string, toId: string, ordered: string[]): void;  // Shift
  dragSelect(ids: string[], additive: boolean): void;                  // 拖拽
  selectAllLoaded(ids: string[]): void;
  clear(): void;
}
```

行为约定（与 outlookEmail 一致，用户无需重新学习）：

- 「全选」只作用于**当前已加载**的项，按钮文案写明「全选已加载」
- 切换分组、改筛选、切换账号 → **清空选择**（防止对筛选外对象误操作）
- 拖拽起点若已选中 → 本次拖拽为**批量取消**
- 选中 ≥1 项时出现批量菜单（`DropdownMenu`）：PC 悬浮在第一个选中项右侧并随滚动重定位；移动端固定底部

选择状态放在 store 而非行组件里，保证虚拟化卸载行时选择不丢。

## 5. 核心页面

### 5.1 `/mail` 三栏工作台（右栏纵向再切）

> 2026-08 改版过一次。原先四栏并列、详情占第四列——
> 三栏挤在 1440 宽里每栏都不够读，现在详情移到右栏下段。

整页是**应用外壳**：撑满视口、自身不滚动、没有 Footer，滚动由各面板自负。
路由用 `handle.shell` 声明这一形态（`src/router/handle.ts`），`Layout` 据此分流。

登录后**没有顶栏**：左侧是常驻导航栏 `AppSidebar`，右侧才是下面这块工作台。
侧栏导航项统一使用相同间距，邮箱、令牌与用量等入口之间不额外插入分组留白。

```
┌───────────────────────────────────────────────────────────────┐
│ MailToolbar：导入/导出/刷新 ‖ 启用/停用/删除(批量)             │ 52px
├──────────────┬───────────────────┬────────────────────────────┤
│ MailSidebar  │ 账号列表          │ 邮件列表                    │
│  账号状态段  │  AccountFilterBar │  FolderTabs(underline)      │
│  分组列表    │  VirtualList      │  + 已读筛选(segmented)      │
│              │  @container 列    ├────────────────────────────┤ ← SplitPane
│              │  Pagination       │ 邮件详情 sandbox iframe     │   可拖拽
├──────────────┴───────────────────┴────────────────────────────┤
│ MailStatusBar：账号/成功/失败/未登录 + 时钟                    │ 30px
└───────────────────────────────────────────────────────────────┘
```

左栏是**两个并列维度**而不是嵌套：状态段筛 `refresh_status`，分组段筛分组，两者可叠加。

右边两栏是**点邮箱地址才出现**的：点列表里的邮箱名打开它的收件箱，
再点一次同一个邮箱就收起（列表行用 `aria-expanded` 表达这个状态），
邮件栏右上角也有一个关闭按钮。移动端那个位置换成「返回账号列表」。

响应式（AGENTS.md §6.6）：

- `≥1280px`：三栏并列，右栏 SplitPane 可拖
- `768~1280px`：两栏（左栏折叠进筛选栏的 `Select`）
- `<768px`：单栏 + 层级导航（账号 → 邮件 → 详情），SplitPane 退化成层级切换、不可拖

账号列表的列数按**容器**宽度切换（`@container`）而不是视口：这一栏夹在中间，
1440 视口下它自己只有 ~570px，按视口断点算会让列宽溢出、画到右栏上去。

组件划分（`web/src/components/mail/`），单个组件不超过 200 行（AGENTS.md §6.1），
列表行用 `React.memo`：

```
MailShell.tsx        MailToolbar.tsx       MailStatusBar.tsx
MailSidebar.tsx      SidebarRow.tsx        StatusDot.tsx
GroupList.tsx        GroupDot.tsx          GroupFormDialog.tsx    GroupDeleteDialog.tsx
AccountList.tsx      AccountFilterBar.tsx  AccountDrawer.tsx      ExportDialog.tsx
MessageList.tsx      MessageRow.tsx        FolderTabs.tsx         MessageFilterPills.tsx
MessageDetail.tsx    MessageBody.tsx       AttachmentList.tsx     MessageBatchBar.tsx
VirtualList.tsx      SplitPane.tsx         EmptyState.tsx
```

账号的批量动作（启用/停用/删除）不在这里，它们和导入/导出/刷新一起收在
`MailToolbar` 里：一个动作只出现在一个地方，用户才不用猜「这两个按钮是不是同一件事」。
改单个账号的配置走 `AccountDrawer`（列表行右侧那支笔）。

### 5.2 `/mail/tokens` Token 刷新

**刷新令牌只在这一页做**（邮箱页上不放第二个入口，否则用户会以为是两件事）。
从上到下六块：

- 统计：总账号 / 正常 / 失败 / 从未刷新，四个数字方块
- 动作条（`LayerCard`）：**刷新全部**、**只刷新失败的（n）**、
  **多选分组 + 刷新选中分组**，任务运行中额外出现「停止」。
  分组下拉选完还要再点一次按钮才提交——下拉一变就发任务，误触的代价是几千次上游调用。
  下拉是 `multiple`（后端的 `group_ids` 本来就收数组）
- 任务面板：`Meter` 进度 + 成功/失败计数 + 逐条结果（邮箱 → 成功/已跳过/失败原因）。
  进度靠 SSE 推送，关掉页面再回来会自动接上仍在跑的那个任务（§7 的 `jobStore`）
- 定时刷新（`RefreshSchedulePanel`，单独一个文件——`TokensPage` 已经接近 200 行的拆分线）：
  一行一个分组，间隔用 `Select` 从固定几档里选（关闭 / 7 天 / 14 天 / 30 天），
  改动即保存，右侧显示后端算出的「下次」时刻。
  不给自由输入的分钟数：这个值没有精调的意义——`refresh_token` 是滑动过期，
  定时刷新要解决的只是「别让它因为长期没人碰而作废」，7 天和 8 天没有可观察的差别；
  而一个空输入框反而会诱使用户填 5 分钟然后被服务商风控。
  保存成功后把分组列表重取一遍，而不是前端自己复算 `next_refresh_at`——
  算出两个不同的时刻只是迟早的事。执行规则见 [05 文档 §6.2](05-api-design.md)
- 刷新失败的账号：分页扫描失败账号，按 `account_type=outlook` 收集并展示最多 200 个账号的具体原因；仅
  `auth_failed` / `consent_required` 提供「重新授权」。弹窗先创建一次性 PKCE 流程，
  再打开 Microsoft；参考应用的回调仍是 localhost 时，用户粘贴地址栏的最终地址，
  换成平台自己的回调域名后则自动完成。代理与应用配置错误按服务端提示处理
- 最近 7 天的失败原因分布：banned / auth_failed / proxy_failed… 各自的处置完全不同，
  这也是把它们分开统计的全部意义

`auth_failed` 的统一标签为「认证失败」，不等同于「令牌失效」；逐账号展示服务端的具体原因。
过期、撤销、重新登录等账号侧问题归 `auth_failed`，客户端配置归 `provider_error`，
权限不足归 `consent_required`。Graph 或 IMAP OAuth 任一通道刷新成功都显示成功。
刷新失败面板展示服务端返回的 Outlook OAuth 失败账号，只有认证/权限问题提供重新授权动作，
代理与应用配置问题按服务端提示处理。

对应后端的 `scope`：`all` / `failed` / `group`（还有一个 `selected` 供别处调用）。
没有 refresh_token 的账号（IMAP 密码账号）四种范围都会自动排除。

### 5.3 `/mail/import` 导入向导

`Dialog` + `Tabs`（三种格式）+ `Code`（格式示例）+ `Input`/`Select`（分组、默认值）。
提交前在前端做一次**解析预览**（前 20 行 + 统计），让用户提交前就发现格式错误。
提交后用 `Banner` 展示结果（成功 N / 更新 N / 跳过 N / 失败 N），
失败明细放可展开的 `Table`。配额导致的跳过要单独用一句话说明，附「查看配额」链接。

### 5.4 `/admin/*` 管理后台

- `/admin/users`：`Table` + `Pagination` + 搜索。行内 `DropdownMenu`：
  禁用/启用、重置密码、调配额、授予管理员、进入其邮箱、删除
- 危险操作（删除用户、重置密码）用 `Dialog` 二次确认，措辞写明影响范围
- `/admin/tenants/:id/mail`：**直接复用 `/mail` 的全部组件**，
  只是 API base 从 `/api/v1/tenants/:tid/mail` 换成 `/api/v1/admin/tenants/:tid/mail`。
  为此把 API 层的 base 路径做成参数（见 §7）
- 管理员进入他人工作空间时，页面顶部**常驻一条 `Banner`**：
  「你正在以管理员身份查看 <用户名> 的工作空间，所有操作都会被记录」——
  防止管理员误以为在自己的空间里操作

### 5.5 `/settings/usage` 配额页

每项配额一个 `Meter`（已用/上限），不限量的显示为 `Badge` 「不限」。
接近上限（≥80%）时整页顶部出 `Banner`。

### 5.6 `/settings/api` 对外取件 API

三块，从上到下：**请求头**（`Authorization: Bearer …`，默认打码只留前缀与末四位，
可显示/复制，重置走同行二次确认）、**接口清单**（五条只读端点的表格）、
**给 Agent**（`/llms.txt` 链接 + 一段可复制的接入说明）。

两个判断：① 明文默认不摆在屏幕上，但打码要留出足够特征，否则用户无法确认
「页面上这把」和「脚本里配的那把」是不是同一把；② 重置用同一行的两个按钮做二次确认，
不用弹窗——这一步要防的是手滑，不是让人重新读一遍说明。

### 5.7 `/resources` 资源推荐

登录后从左侧次级导航进入，按网络与部署、批量业务工具、其他服务分组展示外部资源。
推广链接逐项标记，页面顶部说明返佣关系；外链在新标签页打开并带 `rel="sponsored nofollow"`。
它不进入邮箱、令牌等核心操作流，避免把推荐内容伪装成完成任务所必需的步骤。

## 6. 安全

### 6.1 邮件 HTML 渲染

邮件正文是**完全不可信的第三方输入**，且本平台是多用户 SaaS——
一次 XSS 的爆炸半径是整个租户的邮箱凭据。两层防护缺一不可：

```tsx
const clean = DOMPurify.sanitize(html, {
  FORBID_TAGS: ["script", "iframe", "object", "embed", "form", "style", "link", "base"],
  FORBID_ATTR: ["srcdoc", "formaction"],
  ALLOW_DATA_ATTR: false,
});
```

并且渲染在 **sandboxed iframe** 内（`sandbox="allow-popups allow-popups-to-escape-sandbox"`，
**不给** `allow-scripts`、**不给** `allow-same-origin`），而不是 `dangerouslySetInnerHTML` 到主文档。
outlookEmail 只做了 DOMPurify（同文档渲染），本方案额外加一层隔离。

远程图片默认阻断（iframe 内注入 CSP meta），提供「显示图片」按钮，
避免邮件追踪像素泄露「谁在什么时候看了这封信」。

### 6.2 凭据展示

- 列表/详情只显示 `has_password: true` 之类布尔标志，不回传明文
- 「复制邮箱+别名」（`ClipboardText`）不复制密码
- 导出走一个说明用途的 `Dialog`：界面上明说导出的是**明文**、且这次操作会记进审计
- 代理 URL 在输入框以外一律显示打码版。**输入框里必须是明文**：回填打码串的话，
  用户进来改个名字一按保存，`socks5://u:****@host` 就被当成口令写回库里，
  代理从此是坏的、界面上还看不出来。分组代理的明文因此单独走
  `GET /mail/groups/:id/proxy`（要 `account:secret` 且每次调用写一条审计），
  只在编辑框打开时取一次——列表接口不带明文，否则一进 /mail 就把全部分组的
  代理口令发到浏览器，而绝大多数时候没人要看它们。
  明文没取到时保存要**整组省掉**代理三项（PATCH 不传即保持原值），
  照发一组空输入框等于把用户配好的代理静默清掉

### 6.3 CSRF

模板用 Cookie 会话 + `withCredentials`，目前没有 CSRF token，依赖 `SameSite`。
**P0 必须核查 `pkg/middleware/session.go` 里 Cookie 的 `SameSite` 设置**：
为 `Lax`/`Strict` 则可接受；若因跨域部署设为 `None`，必须补 CSRF token 机制。

## 7. API 层

新增 `web/src/api/mail/`：

```
groups.ts   accounts.ts   messages.ts   jobs.ts
tags.ts     admin.ts      quota.ts
```

风格与现有 `tenant.ts` 一致（对象字面量 + `client.get<ApiResponse<T>>`）。
**base 路径做成参数**，这样管理员视图能直接复用同一套函数：

```ts
// web/src/api/mail/scope.ts
export type MailScope = { kind: "self"; tenantID: string }
                      | { kind: "admin"; tenantID: string };

export const mailBase = (s: MailScope) =>
  s.kind === "admin"
    ? `/api/v1/admin/tenants/${s.tenantID}/mail`
    : `/api/v1/tenants/${s.tenantID}/mail`;

export const accountApi = {
  list: async (s: MailScope, params: AccountListParams) =>
    (await client.get<ApiResponse<Paged<MailAccount>>>(`${mailBase(s)}/accounts`, { params })).data,
  ...
};
```

**注意 `client.ts` 的 `timeout: 10000`**：拉取远端邮件可能超过 10 秒（慢 IMAP + 代理）。
邮件类请求需单独传 `{ timeout: 60000 }`，或在 `mail/messages.ts` 里用一个长超时的 axios 实例。
这是容易漏掉但必然会踩的点。

### 7.1 SSE 客户端

```ts
// web/src/lib/jobStream.ts
export function subscribeJob(base: string, jobID: string, lastEventID: string | null,
                             onEvent: (e: JobEvent) => void): () => void {
  const es = new EventSource(
    `${base}/jobs/${jobID}/stream` + (lastEventID ? `?last_event_id=${lastEventID}` : ""),
    { withCredentials: true },
  );
  ...
}
```

`EventSource` 自带断线重连并携带 `Last-Event-ID` 头，但**无法设置自定义 header**，
所以服务端必须同时接受 query 参数形式（[05 文档](05-api-design.md) §6）。

进度只在 store 里增量累加，**不要**每个 item 事件都重渲整个列表——上万条会卡。
做法：事件写入 ring buffer（保留最近 200 条日志供 `Code` 组件展示），进度数字单独一个 selector。

> **zustand 选择器必须返回稳定引用。** v5 走 `useSyncExternalStore`，
> `useStore((s) => Array.from(s.selected))` 这类每次都新建数组/对象的写法会让 React
> 认定「渲染期间状态一直在变」而无限重渲染——生产构建里就是整页白屏 +
> `Minified React error #185`，开发模式下反而不明显。要别的形状就先取回原引用再 `useMemo`。

## 8. 类型定义

实际落在 `web/src/api/mail.ts`（类型与该域的请求函数放在一起，不另开 `types/` 目录），
与后端 model 的 JSON 标签严格对应：

```ts
export type MailProvider = "outlook" | "gmail" | "qq" | "163" | "126" | "yahoo" | "aliyun" | "2925" | "custom";
export type AuthChannel = "" | "graph" | "imap_new" | "imap_old" | "imap";
export type MailFolder  = "inbox" | "junkemail" | "deleteditems" | "all";
export type RefreshStatus = "never" | "success" | "failed";
export type ErrorKind = "auth_failed" | "banned" | "consent_required" | "proxy_failed"
                      | "network" | "rate_limited" | "folder_unavailable" | "provider_error"
                      | "canceled";
export type PlatformRole = "user" | "admin";
export type GroupColor = "blue" | "green" | "amber" | "red" | "purple" | "gray";
```

后端每次改 model 都要同步这里。类型量级不大，手写即可，不引入代码生成。

## 9. 前端测试

沿用 Vitest + Testing Library（模板已有 `useAsyncAction.test.tsx`、`tenantStore.test.ts` 示例）。
优先覆盖：

| 目标 | 理由 |
|---|---|
| `selectionStore`（Shift 范围、拖拽、清空时机） | 逻辑最绕、bug 最多，纯逻辑好测 |
| `jobStore` 的 SSE 事件归并与断线续接 | 关系到进度正确性 |
| 导入向导的解析预览 | 提交前拦掉脏数据 |
| `MessageBody` 净化 | 安全项，用已知 XSS payload 做回归 |
| `AdminRoute` 守卫 | 非管理员访问 `/admin/*` 应被重定向 |
| `mailBase()` 的 scope 切换 | 保证管理员视图不会误打到自己的租户路径 |

组件快照测试不做（维护成本高、收益低）。

## 10. Kumo 使用注意

1. **组件注册表**：Kumo 提供 AI 可读的组件注册表与 CLI 查文档能力。
   实现页面前先查注册表确认组件的确切 props，不要凭印象写。
2. **`forwardRef` + `displayName`**：自建组件（`VirtualList`、`MessageBody` 等）也遵循这一约定，
   与 Kumo 组件保持一致，方便 DevTools 调试。
3. **包导入**：一律 `import { Button } from "@cloudflare/kumo"`，不写相对路径。
   Kumo 支持粒度导入以优化打包体积，若构建产物偏大再评估切换到子路径导入。
4. **Node 版本**：Kumo 仓库自身要求 Node 24+；作为依赖消费时由 Vite/bun 处理，
   但 CI 的 Node 版本建议对齐到 22+ 以避免 ESM 解析问题。
   `.github/workflows/ci.yml` 需要相应调整。
5. **FedRAMP 主题**：`data-theme="fedramp"` 是 Cloudflare 内部合规主题，本项目不需要，
   使用默认主题即可。
