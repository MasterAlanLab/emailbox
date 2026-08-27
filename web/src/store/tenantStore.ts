import { create } from "zustand";
import { tenantApi, type AuthResponse, type Tenant, type TenantMemberDetail } from "@/api";
interface TenantState {
  tenants: Tenant[];
  activeTenant: Tenant | null;
  membership: TenantMemberDetail | null;
  members: TenantMemberDetail[];
  hydrate: (auth: AuthResponse) => void;
  loadTenants: () => Promise<void>;
  selectTenant: (tenant: Tenant) => Promise<void>;
  loadMembers: () => Promise<void>;
  reset: () => void;
}
export const useTenantStore = create<TenantState>((set, get) => ({
  tenants: [],
  activeTenant: null,
  membership: null,
  members: [],
  // 会话响应已包含租户列表，直接用它填充 store，使刷新后直接进入租户页面也有数据。
  // membership/members 必须一并清空：它们属于上一个会话，换用户登录后会造成越权 UI 和信息泄漏。
  hydrate: (auth) =>
    set({
      tenants: auth.tenants,
      activeTenant:
        auth.tenants.find((t) => t.id === auth.active_tenant_id) ?? auth.tenants[0] ?? null,
      membership: null,
      members: [],
    }),
  loadTenants: async () => {
    const r = await tenantApi.list();
    const currentID = get().activeTenant?.id;
    // 必须取列表里的新对象，沿用旧对象会让改名后的工作区在界面上仍显示旧名字。
    set({
      tenants: r.data,
      activeTenant: r.data.find((t) => t.id === currentID) ?? r.data[0] ?? null,
    });
  },
  selectTenant: async (tenant) => {
    await tenantApi.select(tenant.id);
    set({ activeTenant: tenant, membership: null, members: [] });
    await get().loadMembers();
  },
  loadMembers: async () => {
    const t = get().activeTenant;
    if (!t) return;
    const r = await tenantApi.members(t.id);
    set({
      members: r.data,
      membership: r.data.find((m) => m.user_id === useAuthStore.getState().user?.id) ?? null,
    });
  },
  reset: () => set({ tenants: [], activeTenant: null, membership: null, members: [] }),
}));
import { useAuthStore } from "./authStore";

// 认证状态一旦被清除（主动登出或会话失效），租户数据必须立即作废。
// 否则同一标签页换用户登录时，上一个用户的成员名单和角色会残留在内存中。
useAuthStore.subscribe((state, prev) => {
  if (prev.isAuthenticated && !state.isAuthenticated) {
    useTenantStore.getState().reset();
  }
});
