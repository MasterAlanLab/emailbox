import { act, renderHook } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { useAsyncAction } from "./useAsyncAction";

describe("useAsyncAction", () => {
  it("失败时暴露错误信息并复位 pending", async () => {
    const { result } = renderHook(() => useAsyncAction());
    await act(async () => {
      await result.current.run(async () => {
        throw new Error("原密码错误");
      });
    });
    expect(result.current.error).toBe("原密码错误");
    expect(result.current.pending).toBe(false);
  });

  // 这条是加这个 hook 的主要理由：此前按钮无法禁用，双击"创建工作区"会发出两个请求。
  it("上一次尚未结束时忽略重复提交", async () => {
    const { result } = renderHook(() => useAsyncAction());
    let calls = 0;
    let release!: () => void;
    const blocked = new Promise<void>((resolve) => (release = resolve));

    let first!: Promise<boolean>;
    act(() => {
      first = result.current.run(async () => {
        calls++;
        await blocked;
      });
    });
    expect(result.current.pending).toBe(true);

    await act(async () => {
      // 被忽略的提交返回 false，调用方据此可以区分「没执行」和「执行成功」
      expect(
        await result.current.run(async () => {
          calls++;
        }),
      ).toBe(false);
    });
    expect(calls).toBe(1);

    await act(async () => {
      release();
      await first;
    });
    expect(result.current.pending).toBe(false);
  });

  it("重新提交时清空上一次的错误", async () => {
    const { result } = renderHook(() => useAsyncAction());
    await act(async () => {
      await result.current.run(async () => {
        throw new Error("失败了");
      });
    });
    expect(result.current.error).toBe("失败了");
    await act(async () => {
      await result.current.run(async () => {});
    });
    expect(result.current.error).toBe("");
  });
});
