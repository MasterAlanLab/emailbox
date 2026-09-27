import { beforeEach, describe, expect, it } from "vitest";
import { useAuthStore } from "./authStore";
import { useTenantStore } from "./tenantStore";

const tenant = (id: string) => ({
  id,
  name: id,
  slug: id,
  created_by: "x",
  created_at: "",
  updated_at: "",
});

const user = (id: string) => ({
  id,
  username: id,
  email: `${id}@example.com`,
  status: "active" as const,
  platform_role: "user" as const,
});

describe("会话切换时的租户数据隔离", () => {
  beforeEach(() => {
    useTenantStore.getState().reset();
    useAuthStore.setState({ user: null, isAuthenticated: false, loading: false });
  });

  it("认证失效后自动清空租户数据", () => {
    useAuthStore.setState({ user: user("alice"), isAuthenticated: true, loading: false });
    useTenantStore.setState({
      tenants: [tenant("acme")],
      activeTenant: tenant("acme"),
    });

    useAuthStore.getState().clearAuth();

    const state = useTenantStore.getState();
    expect(state.activeTenant).toBeNull();
    expect(state.tenants).toEqual([]);
  });

  it("hydrate 只使用新会话的租户数据", () => {
    useTenantStore.getState().hydrate({
      user: user("bob"),
      tenants: [tenant("beta")],
      active_tenant_id: "beta",
      desktop: false,
    });

    const state = useTenantStore.getState();
    expect(state.activeTenant?.id).toBe("beta");
    expect(state.tenants).toHaveLength(1);
  });
});
