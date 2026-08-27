import { beforeEach, describe, expect, it } from "vitest";
import type { TenantMemberDetail } from "@/api";
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
  avatar_url: "",
  status: "active" as const,
  platform_role: "user" as const,
});

const member = (userID: string, tenantID: string): TenantMemberDetail => ({
  id: `m-${userID}`,
  tenant_id: tenantID,
  user_id: userID,
  role: "owner",
  created_at: "",
  updated_at: "",
  username: userID,
  email: `${userID}@example.com`,
  avatar_url: "",
});

describe("会话切换时的租户数据隔离", () => {
  beforeEach(() => {
    useTenantStore.getState().reset();
    useAuthStore.setState({ user: null, isAuthenticated: false, loading: false });
  });

  // 同一标签页里换用户登录时，上一个用户的成员名单和角色绝不能残留：
  // 否则新用户会看到别人的邮箱，并短暂获得 owner 专属的管理界面。
  it("认证失效后自动清空租户数据", () => {
    useAuthStore.setState({ user: user("alice"), isAuthenticated: true, loading: false });
    useTenantStore.setState({
      tenants: [tenant("acme")],
      activeTenant: tenant("acme"),
      membership: member("alice", "acme"),
      members: [member("alice", "acme")],
    });

    useAuthStore.getState().clearAuth();

    const state = useTenantStore.getState();
    expect(state.members).toEqual([]);
    expect(state.membership).toBeNull();
    expect(state.activeTenant).toBeNull();
  });

  it("hydrate 不会保留上一个会话的成员信息", () => {
    useTenantStore.setState({
      membership: member("alice", "acme"),
      members: [member("alice", "acme")],
    });

    useTenantStore
      .getState()
      .hydrate({ user: user("bob"), tenants: [tenant("beta")], active_tenant_id: "beta" });

    const state = useTenantStore.getState();
    expect(state.activeTenant?.id).toBe("beta");
    expect(state.members).toEqual([]);
    expect(state.membership).toBeNull();
  });
});
