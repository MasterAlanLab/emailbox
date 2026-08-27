import { NavLink } from "react-router-dom";

// 所有应用页（侧边栏右侧那一块）的统一容器。
//
// 建它是因为改版后量出来的一个问题：内容起始线有**五种**——导入/标签 48px、
// 令牌 96px、设置三页 224px、后台三页 32px，标题字号也有三档。
// 在侧边栏布局里连着切几个页面，标题会横着跳近 200px 还变大小。
//
// 统一的做法是外层一律 `max-w-6xl` 居中：所有页面的标题因此落在同一条竖线上。
// 内容需要更窄（表单类页面读起来才舒服）的，在 children 里自己限宽，
// **不要**去改外层——那样标题会跟着一起挪，问题就又回来了。

interface PageShellProps {
  title: string;
  description?: string;
  // actions 放页面级动作（右上角）。注意别放导航链接：左侧导航栏已经有全站入口了，
  // 再摆一个「返回邮箱」只是噪音。
  actions?: React.ReactNode;
  tabs?: { to: string; label: string; end?: boolean }[];
  children: React.ReactNode;
}

export function PageShell({ title, description, actions, tabs, children }: PageShellProps) {
  return (
    // 移动端收窄内边距：420px 的屏幕上两侧各留 32px 就吃掉近两成宽度。
    <div className="mx-auto w-full max-w-6xl px-4 py-6 sm:px-8 sm:py-8">
      <header className="mb-6">
        <div className="flex flex-wrap items-start justify-between gap-4">
          <div className="min-w-0">
            <h1 className="display-md text-kumo-strong">{title}</h1>
            {description && <p className="mt-2 text-sm text-kumo-subtle">{description}</p>}
          </div>
          {actions}
        </div>

        {tabs && (
          <nav className="mt-5 flex flex-wrap gap-1">
            {tabs.map((tab) => (
              <NavLink
                key={tab.to}
                to={tab.to}
                end={tab.end}
                className={({ isActive }) =>
                  `rounded-lg px-2.5 py-1.5 text-sm ${
                    isActive
                      ? "bg-kumo-tint font-medium text-kumo-strong"
                      : "text-kumo-subtle hover:bg-kumo-tint hover:text-kumo-strong"
                  }`
                }
              >
                {tab.label}
              </NavLink>
            ))}
          </nav>
        )}
      </header>

      {children}
    </div>
  );
}
