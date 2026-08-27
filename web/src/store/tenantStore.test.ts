import { beforeEach, describe, expect, it } from "vitest";
import type { AuthResponse } from "@/api";
import { useTenantStore } from "./tenantStore";

const user = {
  id: "u1",
  username: "alice",
  email: "a@example.com",
  status: "active" as const,
  platform_role: "user" as const,
};

function tenant(id: string) {
  return { id, name: id, slug: id, created_by: "u1", created_at: "", updated_at: "" };
}

function auth(tenants: ReturnType<typeof tenant>[], active: string | null): AuthResponse {
  return { user, tenants, active_tenant_id: active };
}

// hydrate 的三段回退是整个应用里分支最多、也最容易出错的一处：
// 会话里的 active_tenant_id 可能指向已删除的租户，也可能一个租户都没有。
describe("tenantStore.hydrate", () => {
  beforeEach(() => useTenantStore.getState().reset());

  it("选中 active_tenant_id 指向的租户", () => {
    useTenantStore.getState().hydrate(auth([tenant("a"), tenant("b")], "b"));
    expect(useTenantStore.getState().activeTenant?.id).toBe("b");
    expect(useTenantStore.getState().tenants).toHaveLength(2);
  });

  it("active_tenant_id 不在列表中时回退到第一个租户", () => {
    useTenantStore.getState().hydrate(auth([tenant("a")], "已删除的租户"));
    expect(useTenantStore.getState().activeTenant?.id).toBe("a");
  });

  it("没有任何租户时为 null，而不是 undefined", () => {
    useTenantStore.getState().hydrate(auth([], "已删除的租户"));
    expect(useTenantStore.getState().activeTenant).toBeNull();
  });
});
