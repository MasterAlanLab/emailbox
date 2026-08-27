import { beforeEach, describe, expect, it, vi } from "vitest";
import { userApi } from "@/api";
import { useAuthStore } from "./authStore";

// 会话恢复的并发去重。
//
// 这条不是假想的场景：StrictMode 在开发模式下会把 effect 跑两遍，
// 没有去重就是两次一模一样的 /auth/session。加回一处「顺手也调一下 loadSession」
// 的代码同样会让请求翻倍，而这种事在浏览器里不看网络面板根本发现不了。
describe("loadSession 的并发去重", () => {
  beforeEach(() => {
    useAuthStore.setState({ user: null, isAuthenticated: false, loading: true });
    vi.restoreAllMocks();
  });

  it("同时调用多次只打一次接口，且都拿到同一份结果", async () => {
    const payload = {
      user: { id: "u1", username: "alice", email: "alice@example.com" },
      tenants: [],
    };
    const spy = vi
      .spyOn(userApi, "session")
      .mockResolvedValue({ code: 0, data: payload, message: "" } as never);

    const results = await Promise.all([
      useAuthStore.getState().loadSession(),
      useAuthStore.getState().loadSession(),
      useAuthStore.getState().loadSession(),
    ]);

    expect(spy).toHaveBeenCalledTimes(1);
    expect(results.every((r) => r === results[0])).toBe(true);
    expect(useAuthStore.getState().isAuthenticated).toBe(true);
  });

  it("上一次结束后再调会重新取，不会拿到过期的缓存", async () => {
    const spy = vi.spyOn(userApi, "session").mockRejectedValue(new Error("401"));

    await useAuthStore.getState().loadSession();
    expect(useAuthStore.getState().isAuthenticated).toBe(false);

    // 登出再登录属于这条路径：第二次必须真的再问一次服务端。
    await useAuthStore.getState().loadSession();
    expect(spy).toHaveBeenCalledTimes(2);
  });
});
