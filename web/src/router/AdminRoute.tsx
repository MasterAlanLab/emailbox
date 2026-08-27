import { Navigate } from "react-router-dom";
import { useAuthStore } from "@/store/authStore";
import { ProtectedRoute } from "./RouteGuards";

// AdminRoute 只负责「别把后台入口摆在普通用户面前」。
//
// 它不是安全边界：任何人都能改本地 state 让这个判断通过。真正的拦截在服务端的
// RequirePlatformAdmin，每个 /admin/* 端点都有一条 403 测试守着（api/admin_test.go）。
// 这里重定向到 /mail 而不是 /login：用户已经登录了，让他回登录页只会更困惑。
export function AdminRoute({ children }: { children: React.ReactNode }) {
  return (
    <ProtectedRoute>
      <AdminOnly>{children}</AdminOnly>
    </ProtectedRoute>
  );
}

function AdminOnly({ children }: { children: React.ReactNode }) {
  const user = useAuthStore((s) => s.user);
  if (!user) return null;
  return user.platform_role === "admin" ? <>{children}</> : <Navigate to="/mail" replace />;
}
