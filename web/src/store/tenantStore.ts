import { create } from "zustand";
import type { AuthResponse, Tenant } from "@/api";
interface TenantState {
  tenants: Tenant[];
  activeTenant: Tenant | null;
  hydrate: (auth: AuthResponse) => void;
  reset: () => void;
}
export const useTenantStore = create<TenantState>((set) => ({
  tenants: [],
  activeTenant: null,
  // 会话响应已包含租户列表，直接用它填充 store，使刷新后直接进入租户页面也有数据。
  hydrate: (auth) =>
    set({
      tenants: auth.tenants,
      activeTenant:
        auth.tenants.find((t) => t.id === auth.active_tenant_id) ?? auth.tenants[0] ?? null,
    }),
  reset: () => set({ tenants: [], activeTenant: null }),
}));
import { useAuthStore } from "./authStore";

// 认证状态一旦被清除（主动登出或会话失效），租户数据必须立即作废。
// 否则同一标签页换用户登录时，上一个用户的租户列表会残留在内存中。
useAuthStore.subscribe((state, prev) => {
  if (prev.isAuthenticated && !state.isAuthenticated) {
    useTenantStore.getState().reset();
  }
});
