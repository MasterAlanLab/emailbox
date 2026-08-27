import client from "@/lib/client";
import type { ApiResponse } from "@/lib/client";
export interface User {
  id: string;
  username: string;
  email: string;
  status: "active" | "disabled";
  // 与租户角色正交：它决定能否跨租户管理整个系统。前端只用它决定是否显示后台入口，
  // 真正的拦截在服务端的 RequirePlatformAdmin —— 改前端 state 什么也拿不到。
  platform_role: "user" | "admin";
}
export interface Tenant {
  id: string;
  name: string;
  slug: string;
  created_by: string;
  created_at: string;
  updated_at: string;
}
export interface AuthResponse {
  user: User;
  tenants: Tenant[];
  active_tenant_id: string | null;
}
// 注册只要用户名和密码。邮箱是登录后可填、也可以一直不填的资料字段。
export interface RegisterRequest {
  username: string;
  password: string;
}
// 登录认用户名：邮箱可以不填，一个可选字段当不了凭据。
export interface LoginRequest {
  username: string;
  password: string;
}
export const userApi = {
  register: async (data: RegisterRequest) =>
    (await client.post<ApiResponse<AuthResponse>>("/api/v1/auth/register", data)).data,
  login: async (data: LoginRequest) =>
    (await client.post<ApiResponse<AuthResponse>>("/api/v1/auth/login", data)).data,
  logout: async () => (await client.post<ApiResponse<null>>("/api/v1/auth/logout")).data,
  session: async () => (await client.get<ApiResponse<AuthResponse>>("/api/v1/auth/session")).data,
  profile: async () => (await client.get<ApiResponse<User>>("/api/v1/user/profile")).data,
  updateProfile: async (data: Partial<Pick<User, "username" | "email">>) =>
    (await client.patch<ApiResponse<User>>("/api/v1/user/profile", data)).data,
  changePassword: async (data: { old_password: string; new_password: string }) =>
    (await client.post<ApiResponse<null>>("/api/v1/user/change-password", data)).data,
};
