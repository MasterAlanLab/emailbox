import { Navigate } from "react-router-dom";
import { useAuthStore } from "@/store/authStore";

// 两个守卫都**只读**会话状态，不自己发起恢复——那件事由 Layout 统一做一次。
// 各自调 loadSession 的旧写法有两个毛病：同一时刻会打两次 /auth/session，
// 而且没挂守卫的页面（首页）根本不会恢复会话。

export function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const loading = useAuthStore((s) => s.loading);
  if (loading) return <div className="p-12 text-center text-sm text-kumo-subtle">正在载入…</div>;
  return isAuthenticated ? <>{children}</> : <Navigate to="/login" replace />;
}

export function PublicRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const loading = useAuthStore((s) => s.loading);
  if (loading) return <div className="p-12 text-center text-sm text-kumo-subtle">正在载入…</div>;
  return isAuthenticated ? <Navigate to="/mail" replace /> : <>{children}</>;
}
