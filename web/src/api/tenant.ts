import client from "@/lib/client";
import type { ApiResponse } from "@/lib/client";
import type { Limits } from "./mail";
import type { Tenant, User } from "./user";
export type TenantRole = "owner" | "admin" | "member";
export interface TenantMember {
  id: string;
  tenant_id: string;
  user_id: string;
  role: TenantRole;
  created_at: string;
  updated_at: string;
}
// 成员列表接口额外返回用户信息，对应 Go 侧的 TenantMemberDetail。
export interface TenantMemberDetail extends TenantMember {
  username: string;
  email: string;
}
// QuotaUsage 是配额页要展示的一切：生效上限 + 当前用量。
// 与后端 service.QuotaUsage 手工同步。
export interface QuotaUsage {
  limits: Limits;
  usage: {
    accounts: number;
    groups: number;
    mail_fetch: number;
    token_refresh: number;
  };
  // 频次类用量的统计日期（按租户时区），供前端说明「明日重置」。
  day: string;
}

export interface BillingPlanPrice {
  id: string;
  plan_id: string;
  plan_code: string;
  plan_name: string;
  billing_period: "monthly" | "yearly";
  currency: string;
  amount: string;
  provider_product_id?: string;
  sync_status: "pending" | "active" | "error" | "inactive";
  active: boolean;
}

export interface BillingSubscription {
  id: string;
  plan_id: string;
  plan_price_id: string;
  status: "pending" | "active" | "canceling" | "past_due" | "canceled" | "expired";
  currency: string;
  amount: string;
  current_period_end?: string;
  cancel_at_period_end: boolean;
}

export const tenantApi = {
  quota: async (id: string) =>
    (await client.get<ApiResponse<QuotaUsage>>(`/api/v1/tenants/${id}/quota`)).data,
  billingPlans: async (id: string) =>
    (await client.get<ApiResponse<BillingPlanPrice[]>>(`/api/v1/tenants/${id}/billing/plans`)).data,
  billingSubscription: async (id: string) =>
    (
      await client.get<ApiResponse<BillingSubscription | null>>(
        `/api/v1/tenants/${id}/billing/subscription`,
      )
    ).data,
  checkout: async (id: string, data: { plan_price_id: string; idempotency_key: string }) =>
    (
      await client.post<ApiResponse<{ checkout_url: string; checkout_id: string }>>(
        `/api/v1/tenants/${id}/billing/checkout`,
        data,
        // 服务端要依次调 Waffo 建会话、签令牌，默认 10 秒不够稳。
        { timeout: 30_000 },
      )
    ).data,
  // 向 Waffo 核对未确认的结账并返回当前订阅；changed 表示这次对账改了订阅（配额要重取）。
  syncBilling: async (id: string) =>
    (
      await client.post<
        ApiResponse<{ subscription: BillingSubscription | null; changed: boolean }>
      >(`/api/v1/tenants/${id}/billing/sync`, {}, { timeout: 30_000 })
    ).data,
  cancelBilling: async (id: string) =>
    (await client.post<ApiResponse<null>>(`/api/v1/tenants/${id}/billing/cancel`)).data,
  uncancelBilling: async (id: string) =>
    (await client.post<ApiResponse<null>>(`/api/v1/tenants/${id}/billing/uncancel`)).data,
  list: async () => (await client.get<ApiResponse<Tenant[]>>("/api/v1/tenants")).data,
  create: async (data: { name: string; slug: string }) =>
    (await client.post<ApiResponse<Tenant>>("/api/v1/tenants", data)).data,
  update: async (id: string, data: { name: string; slug: string }) =>
    (await client.patch<ApiResponse<Tenant>>(`/api/v1/tenants/${id}`, data)).data,
  remove: async (id: string) =>
    (await client.delete<ApiResponse<null>>(`/api/v1/tenants/${id}`)).data,
  select: async (id: string) =>
    (await client.post<ApiResponse<null>>(`/api/v1/tenants/${id}/select`)).data,
  members: async (id: string) =>
    (await client.get<ApiResponse<TenantMemberDetail[]>>(`/api/v1/tenants/${id}/members`)).data,
  // 按用户名加人：邮箱是可选字段，按它找的话没填邮箱的同事根本加不进来。
  addMember: async (id: string, data: { username: string; role: TenantRole }) =>
    (await client.post<ApiResponse<TenantMember>>(`/api/v1/tenants/${id}/members`, data)).data,
  updateMember: async (id: string, userID: string, role: TenantRole) =>
    (await client.patch<ApiResponse<null>>(`/api/v1/tenants/${id}/members/${userID}`, { role }))
      .data,
  removeMember: async (id: string, userID: string) =>
    (await client.delete<ApiResponse<null>>(`/api/v1/tenants/${id}/members/${userID}`)).data,
};
export type { Tenant, User };
