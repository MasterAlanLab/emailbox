import client from "@/lib/client";
import type { ApiResponse } from "@/lib/client";

// 与后端 model.APIKeyView 手工同步。明文只在这一个接口上出现。
export interface ApiKey {
  token: string;
  created_at: string;
  updated_at: string;
}

export const apiKeyApi = {
  // 还没生成时后端回 data:null，页面据此显示「生成」而不是「重置」。
  get: async (tenantID: string) =>
    (await client.get<ApiResponse<ApiKey | null>>(`/api/v1/tenants/${tenantID}/api-key`)).data,
  reset: async (tenantID: string) =>
    (await client.post<ApiResponse<ApiKey>>(`/api/v1/tenants/${tenantID}/api-key/reset`)).data,
};
