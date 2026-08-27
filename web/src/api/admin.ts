import client from "@/lib/client";
import type { ApiResponse } from "@/lib/client";
import type { Limits, Pagination } from "./mail";

// 与后端 pkg/model/admin.go、audit.go 手工同步。

export interface AdminUser {
  id: string;
  username: string;
  email: string;
  status: "active" | "disabled";
  platform_role: "user" | "admin";
  created_at: string;
  last_login_at: string | null;
  tenant_id: string;
  tenant_name: string;
  account_count: number;
  plan_code: string;
  max_accounts: number;
  // 调低配额不追溯删除已有数据，因此「已经超额」是合法且会长期存在的状态。
  over_quota: boolean;
}

export interface AdminTenant {
  id: string;
  name: string;
  slug: string;
  kind: "personal" | "team";
  created_at: string;
  owner_user_id: string;
  owner_email: string;
  plan_code: string;
  max_accounts: number;
  account_count: number;
  // 调低配额不追溯删除已有数据，因此「已经超额」是合法且会长期存在的状态。
  over_quota: boolean;
}

export interface Plan {
  id: string;
  code: string;
  name: string;
  is_default: boolean;
  max_accounts: number;
  max_groups: number;
  daily_mail_fetch: number;
  daily_token_refresh: number;
  created_at: string;
  updated_at: string;
}

export interface AuditLog {
  id: string;
  tenant_id: string;
  actor_user_id: string;
  // 存的是用户名而不是邮箱：邮箱可选，用它做「用户被删后仍能追溯是谁」
  // 这层兜底，会在最需要追溯的时候正好是空的。
  actor_name: string;
  actor_kind: "user" | "admin" | "api_key" | "system";
  action: string;
  resource_type: string;
  resource_id: string;
  ip: string;
  details: string;
  created_at: string;
}

export interface PlatformStats {
  user_count: number;
  disabled_user_count: number;
  admin_count: number;
  tenant_count: number;
  account_count: number;
  banned_account_count: number;
  mail_fetch_today: number;
  token_refresh_today: number;
}

interface Page<T> {
  items: T[];
  pagination: Pagination;
}

export interface AdminQuotaUpdate {
  plan_id?: string;
  note: string;
  max_accounts?: number | null;
  max_groups?: number | null;
  daily_mail_fetch?: number | null;
  daily_token_refresh?: number | null;
}

export interface TenantQuotaUsage {
  limits: Limits;
  usage: {
    accounts: number;
    groups: number;
    mail_fetch: number;
    token_refresh: number;
  };
  day: string;
}

const base = "/api/v1/admin";

export const adminApi = {
  stats: async () => (await client.get<ApiResponse<PlatformStats>>(`${base}/stats`)).data,

  users: async (params: { q?: string; status?: string; platform_role?: string; page?: number }) =>
    (await client.get<ApiResponse<Page<AdminUser>>>(`${base}/users`, { params })).data,
  user: async (userID: string) =>
    (await client.get<ApiResponse<AdminUser>>(`${base}/users/${userID}`)).data,
  updateUser: async (userID: string, data: { status?: string; platform_role?: string }) =>
    (await client.patch<ApiResponse<AdminUser>>(`${base}/users/${userID}`, data)).data,
  // 临时密码只在这一次响应里出现，页面必须当场显示给管理员，不能只存进 state 就算了。
  resetPassword: async (userID: string) =>
    (await client.post<ApiResponse<{ password: string }>>(`${base}/users/${userID}/reset-password`))
      .data,
  deleteUser: async (userID: string) =>
    (await client.delete<ApiResponse<{ deleted_accounts: number }>>(`${base}/users/${userID}`))
      .data,

  tenants: async (params: { q?: string; page?: number }) =>
    (await client.get<ApiResponse<Page<AdminTenant>>>(`${base}/tenants`, { params })).data,
  tenantQuota: async (tenantID: string) =>
    (await client.get<ApiResponse<TenantQuotaUsage>>(`${base}/tenants/${tenantID}/quota`)).data,
  updateTenantQuota: async (tenantID: string, data: AdminQuotaUpdate) =>
    (await client.patch<ApiResponse<TenantQuotaUsage>>(`${base}/tenants/${tenantID}/quota`, data))
      .data,

  plans: async () => (await client.get<ApiResponse<Plan[]>>(`${base}/plans`)).data,
  createPlan: async (data: Partial<Plan>) =>
    (await client.post<ApiResponse<Plan>>(`${base}/plans`, data)).data,
  updatePlan: async (planID: string, data: Partial<Plan>) =>
    (await client.patch<ApiResponse<Plan>>(`${base}/plans/${planID}`, data)).data,
  deletePlan: async (planID: string) =>
    (await client.delete<ApiResponse<null>>(`${base}/plans/${planID}`)).data,

  audit: async (params: {
    tenant_id?: string;
    actor_user_id?: string;
    actor_kind?: string;
    action?: string;
    page?: number;
  }) => (await client.get<ApiResponse<Page<AuditLog>>>(`${base}/audit`, { params })).data,
};
