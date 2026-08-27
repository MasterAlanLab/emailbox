import { createBrowserRouter } from "react-router-dom";
import { Layout } from "@/components/layout";
import { AdminRoute } from "./AdminRoute";
import { appRoute, shellRoute } from "./handle";
import { ProtectedRoute, PublicRoute } from "./RouteGuards";
import HomePage from "@/pages/HomePage";
import LoginPage from "@/pages/LoginPage";
import RegisterPage from "@/pages/RegisterPage";
import ProfileSettingsPage from "@/pages/ProfileSettingsPage";
import SecuritySettingsPage from "@/pages/SecuritySettingsPage";
import PrivacyPage from "@/pages/PrivacyPage";
import TermsPage from "@/pages/TermsPage";
import ErrorPage from "@/pages/ErrorPage";
import MailPage from "@/pages/mail/MailPage";
import GroupsPage from "@/pages/mail/GroupsPage";
import ImportPage from "@/pages/mail/ImportPage";
import TokensPage from "@/pages/mail/TokensPage";
import UsagePage from "@/pages/UsagePage";
import AdminOverviewPage from "@/pages/admin/AdminOverviewPage";
import AdminUsersPage from "@/pages/admin/AdminUsersPage";
import AdminPlansPage from "@/pages/admin/AdminPlansPage";
import AdminAuditPage from "@/pages/admin/AdminAuditPage";
import AdminTenantMailPage from "@/pages/admin/AdminTenantMailPage";

// 工作区（租户）在**前端不露面**：一个用户就是一个工作区，界面上不提这个概念。
// 后端的多租户模型、API 的 tenantID、tenantStore 全部原样保留——
// 以后要做团队协作时，把 TenantSettingsPage / TenantMembersPage 挂回路由即可
// （两个页面文件都留着，只是现在没有入口）。
const protect = (node: React.ReactNode) => <ProtectedRoute>{node}</ProtectedRoute>;
// 后台入口对普通用户隐藏。真正的拦截在服务端，这里只是不把门摆出来。
const admin = (node: React.ReactNode) => <AdminRoute>{node}</AdminRoute>;

export const router = createBrowserRouter([
  {
    path: "/",
    element: <Layout />,
    errorElement: <ErrorPage />,
    children: [
      // 公开页：无 handle，走顶栏 + 文档流那一套。
      { index: true, element: <HomePage /> },
      {
        path: "login",
        element: (
          <PublicRoute>
            <LoginPage />
          </PublicRoute>
        ),
      },
      {
        path: "register",
        element: (
          <PublicRoute>
            <RegisterPage />
          </PublicRoute>
        ),
      },
      { path: "legal/privacy-policy", element: <PrivacyPage /> },
      { path: "legal/terms", element: <TermsPage /> },

      // 应用页：左侧导航栏 + 右侧内容区，没有顶栏。
      // 两个邮箱工作台再带 shell——内容区自身不滚动。
      { path: "mail", element: protect(<MailPage />), handle: shellRoute },
      { path: "mail/import", element: protect(<ImportPage />), handle: appRoute },
      { path: "mail/groups", element: protect(<GroupsPage />), handle: appRoute },
      { path: "mail/tokens", element: protect(<TokensPage />), handle: appRoute },
      { path: "settings/profile", element: protect(<ProfileSettingsPage />), handle: appRoute },
      { path: "settings/security", element: protect(<SecuritySettingsPage />), handle: appRoute },
      { path: "settings/usage", element: protect(<UsagePage />), handle: appRoute },
      { path: "admin", element: admin(<AdminOverviewPage />), handle: appRoute },
      { path: "admin/users", element: admin(<AdminUsersPage />), handle: appRoute },
      {
        path: "admin/tenants/:tenantID/mail",
        element: admin(<AdminTenantMailPage />),
        handle: shellRoute,
      },
      { path: "admin/plans", element: admin(<AdminPlansPage />), handle: appRoute },
      { path: "admin/audit", element: admin(<AdminAuditPage />), handle: appRoute },
    ],
  },
]);
