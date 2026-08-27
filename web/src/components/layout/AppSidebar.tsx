import {
  CaretLeft,
  CaretRight,
  Code,
  Envelope,
  FolderOpen,
  Gauge,
  Key,
  Moon,
  ShieldCheck,
  SignOut,
  Sun,
  type Icon,
} from "@phosphor-icons/react";
import { useState } from "react";
import { Link, NavLink, useNavigate } from "react-router-dom";
import { currentTheme, setTheme, type ResolvedTheme } from "@/lib/theme";
import { useAuthStore } from "@/store/authStore";

// 登录后的常驻导航栏。取代了原来的顶栏——所有全局入口都收在这里。
//
// 可伸缩：展开 224px（图标 + 文字），收起 56px（只剩图标，靠 title 提示）。
// 收起状态记在 localStorage，因为它是很强的个人偏好：屏幕小的人会一直收着，
// 每次进来都要再点一次的话很烦人。

const STORAGE_KEY = "emailbox.nav.collapsed";

interface NavItem {
  to: string;
  label: string;
  icon: Icon;
  end?: boolean;
  adminOnly?: boolean;
}

const PRIMARY: NavItem[] = [
  { to: "/mail", label: "邮箱", icon: Envelope, end: true },
  { to: "/mail/groups", label: "分组", icon: FolderOpen },
  { to: "/mail/tokens", label: "令牌", icon: Key },
];

const SECONDARY: NavItem[] = [
  { to: "/settings/usage", label: "用量", icon: Gauge },
  { to: "/settings/api", label: "API", icon: Code },
  { to: "/admin", label: "后台", icon: ShieldCheck, adminOnly: true },
];

export function AppSidebar() {
  const user = useAuthStore((state) => state.user);
  const logout = useAuthStore((state) => state.logout);
  const navigate = useNavigate();
  const [collapsed, setCollapsed] = useState(restoreCollapsed);
  const [theme, setLocalTheme] = useState<ResolvedTheme>(currentTheme);

  const toggleCollapsed = () => {
    setCollapsed((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(STORAGE_KEY, String(next));
      } catch {
        // 隐私模式下写不了。记不住偏好无所谓，不能因此让按钮点了没反应。
      }
      return next;
    });
  };

  const items = [...PRIMARY, ...SECONDARY].filter(
    // 后台入口只给平台管理员看。这不是权限控制——每个 /admin/* 端点在服务端
    // 都有 RequirePlatformAdmin 把着，这里只是不把门摆出来。
    (item) => !item.adminOnly || user?.platform_role === "admin",
  );

  // 窄屏**强制**图标模式：420px 的屏幕上，一条 224px 的导航栏会吃掉一半宽度，
  // 剩下的空间连账号列表都放不下。用 CSS 断点而不是 matchMedia——
  // JS 里再写一个 768 就多了一个真源，迟早和 Tailwind 的断点对不上。
  // 展开宽度只在 md 以上生效，用户的收起偏好也只在那时才有意义。
  const widthClass = collapsed ? "w-14" : "w-14 md:w-(--ebx-nav-w)";
  const labelClass = collapsed ? "hidden" : "hidden md:block";

  return (
    <nav
      aria-label="主导航"
      className={`flex shrink-0 flex-col border-r border-kumo-line bg-kumo-canvas transition-[width] duration-200 ${widthClass}`}
    >
      <Link
        to="/mail"
        className="flex h-14 shrink-0 items-center gap-2.5 px-3.5 text-kumo-strong"
        title="Emailbox"
      >
        <span className="grid size-7 shrink-0 place-items-center rounded-lg bg-linear-to-br from-ebx-brand-grad-from to-ebx-brand-grad-to text-white">
          <Envelope size={15} weight="fill" />
        </span>
        <span className={`${labelClass} truncate text-sm font-semibold tracking-[-0.01em]`}>
          Emailbox
        </span>
      </Link>

      <div className="flex min-h-0 flex-1 flex-col gap-0.5 overflow-y-auto px-2 py-2">
        {items.map((item, index) => (
          <div key={item.to} className="contents">
            {/* 主导航和次级入口之间留一条空隙，不画分隔线——
                Linear 靠间距分组，多一条线就多一分噪音。 */}
            {index === PRIMARY.length && <div className="h-3" aria-hidden />}
            <SidebarLink item={item} collapsed={collapsed} labelClass={labelClass} />
          </div>
        ))}
      </div>

      <div className="shrink-0 border-t border-kumo-line p-2">
        <SidebarButton
          icon={theme === "dark" ? Sun : Moon}
          label={theme === "dark" ? "亮色主题" : "暗色主题"}
          collapsed={collapsed}
          labelClass={labelClass}
          onClick={() => setLocalTheme(setTheme(theme === "dark" ? "light" : "dark"))}
        />
        <SidebarButton
          icon={collapsed ? CaretRight : CaretLeft}
          label="收起导航"
          collapsed={collapsed}
          labelClass={labelClass}
          // 窄屏本来就是图标模式，再给一个「收起」按钮没有意义。
          className="hidden md:flex"
          onClick={toggleCollapsed}
        />

        <div className="mt-2 border-t border-kumo-line pt-2">
          <NavLink
            to="/settings/profile"
            title={user?.email}
            className={({ isActive }) =>
              `flex min-h-9 items-center gap-2.5 rounded-lg px-2 text-sm ${
                isActive
                  ? "bg-kumo-tint text-kumo-strong"
                  : "text-kumo-default hover:bg-kumo-tint hover:text-kumo-strong"
              }`
            }
          >
            <span className="grid size-6 shrink-0 place-items-center rounded-full bg-kumo-brand text-[11px] font-semibold text-white uppercase">
              {initial(user)}
            </span>
            <span className={`${labelClass} min-w-0 flex-1 truncate`}>{user?.username}</span>
          </NavLink>
          <SidebarButton
            icon={SignOut}
            label="退出"
            collapsed={collapsed}
            labelClass={labelClass}
            // 登出请求失败也要离开当前页：本地会话状态已清空，
            // 停留在原地会让用户看到一个自己已无权访问的页面。
            onClick={() => {
              logout()
                .catch(() => {})
                .finally(() => navigate("/"));
            }}
          />
        </div>
      </div>
    </nav>
  );
}

function SidebarLink({
  item,
  collapsed,
  labelClass,
}: {
  item: NavItem;
  collapsed: boolean;
  labelClass: string;
}) {
  const { icon: IconComponent } = item;
  return (
    <NavLink
      to={item.to}
      end={item.end}
      // 收起时文字没了，title 是唯一的说明。
      title={collapsed ? item.label : undefined}
      className={({ isActive }) =>
        `flex min-h-9 items-center gap-2.5 rounded-lg px-2 text-sm ${
          isActive
            ? "bg-kumo-tint font-medium text-kumo-strong"
            : "text-kumo-default hover:bg-kumo-tint hover:text-kumo-strong"
        }`
      }
    >
      <IconComponent size={17} className="shrink-0" />
      <span className={`${labelClass} truncate`}>{item.label}</span>
    </NavLink>
  );
}

function SidebarButton({
  icon: IconComponent,
  label,
  collapsed,
  labelClass,
  className = "",
  onClick,
}: {
  icon: Icon;
  label: string;
  collapsed: boolean;
  labelClass: string;
  className?: string;
  onClick: () => void;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      title={collapsed ? label : undefined}
      aria-label={label}
      className={`flex min-h-9 w-full items-center gap-2.5 rounded-lg px-2 text-sm text-kumo-default hover:bg-kumo-tint hover:text-kumo-strong ${className}`}
    >
      <IconComponent size={17} className="shrink-0" />
      <span className={`${labelClass} truncate`}>{label}</span>
    </button>
  );
}

function restoreCollapsed(): boolean {
  try {
    return localStorage.getItem(STORAGE_KEY) === "true";
  } catch {
    return false;
  }
}

// 取一个用来当头像的字符。用户名可能是中文，取首字即可；
// 都取不到时退回邮箱首字母，而不是留一个空圆圈。
function initial(user: { username?: string; email?: string } | null): string {
  return (user?.username || user?.email || "?").trim().charAt(0);
}
