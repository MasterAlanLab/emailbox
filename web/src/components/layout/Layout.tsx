import { useEffect } from "react";
import { Outlet, useMatches } from "react-router-dom";
import type { RouteHandle } from "@/router/handle";
import { useAuthStore } from "@/store/authStore";
import { useTenantStore } from "@/store/tenantStore";
import { AppSidebar } from "./AppSidebar";
import { PublicHeader } from "./PublicHeader";
import { Footer } from "./Footer";

// 两种页面形态，由路由 handle 决定（见 src/router/handle.ts）：
//
// - **应用页**（`handle.app`）：左侧常驻导航栏 + 右侧内容区，**没有顶栏**。
//   全局入口、主题切换、账号都收在导航栏里。整体撑满视口。
//   其中带 `handle.shell` 的（/mail 工作台）内容区自身也不滚动，
//   滚动交给它内部的各个面板；其余应用页内容区正常滚动。
// - **公开页**（无 handle）：首页、登录、注册、法务。极简顶栏 + 文档流 + 页脚。
//   这些页面上没有导航栏可挂，登录入口必须摆在顶上。
//
// 高度用 dvh 不用 vh：移动端 Safari 的 100vh 不含地址栏，
// 会让内容底部被浏览器 UI 压住。
export const Layout = () => {
  // 会话恢复统一在这里做一次。
  //
  // 以前是 ProtectedRoute / PublicRoute 各自调 loadSession，于是**没有守卫的页面
  // 从来不恢复会话**——首页就是这样：已经登录的人打开它，authStore 里仍是未登录，
  // 顶栏和 Hero 都显示「登录 / 免费开始」，点下去才被守卫弹回工作台。
  // 放在 Layout 里，所有页面（含公开页）都能拿到真实的登录态。
  const loading = useAuthStore((state) => state.loading);
  const loadSession = useAuthStore((state) => state.loadSession);
  useEffect(() => {
    if (!loading) return;
    // 租户 store 一并填充：直接刷新某个 /mail 页面时，它要靠这份数据才知道请求哪个租户。
    void loadSession().then((auth) => {
      if (auth) useTenantStore.getState().hydrate(auth);
    });
  }, [loading, loadSession]);

  const matches = useMatches();
  const handle = (match: (typeof matches)[number]) => match.handle as RouteHandle | undefined;
  const app = matches.some((match) => handle(match)?.app);
  const shell = matches.some((match) => handle(match)?.shell);

  if (app) {
    return (
      <div className="flex h-dvh overflow-hidden bg-kumo-base">
        <AppSidebar />
        {/* min-w-0 不能少：内容区里有虚拟列表和长邮箱地址，
            flex 子项默认按内容撑宽，缺了它会把导航栏挤出屏幕。 */}
        <main
          className={
            shell
              ? "flex min-h-0 min-w-0 flex-1 flex-col"
              : "min-w-0 flex-1 overflow-y-auto overflow-x-hidden"
          }
        >
          <Outlet />
        </main>
      </div>
    );
  }

  return (
    <div className="flex min-h-screen flex-col bg-kumo-base">
      <PublicHeader />
      {/* flex 容器：登录/错误这类要垂直居中的页面用 flex-1 填满即可，
          不用再写 calc(100vh - 顶栏 - 页脚) 那种一改布局就失效的魔法数。 */}
      <main className="flex flex-1 flex-col">
        <Outlet />
      </main>
      <Footer />
    </div>
  );
};
