import { beforeEach, describe, expect, it } from "vitest";
import { useSelectionStore } from "./selectionStore";

describe("selectionStore", () => {
  beforeEach(() => {
    useSelectionStore.getState().clear();
  });

  it("切换单个选中状态", () => {
    const { toggle } = useSelectionStore.getState();
    toggle("a");
    expect(useSelectionStore.getState().isSelected("a")).toBe(true);
    toggle("a");
    expect(useSelectionStore.getState().isSelected("a")).toBe(false);
  });

  // 「全选」只能作用于当前页已加载的 id。若它悄悄把未加载的行也算进去，
  // 用户以为删了 50 条，实际会删掉几千条。
  it("全选只作用于传入的当前页 id", () => {
    useSelectionStore.getState().selectPage(["a", "b"]);
    expect(useSelectionStore.getState().ids().sort()).toEqual(["a", "b"]);

    // 翻到下一页再全选，累加而不是覆盖
    useSelectionStore.getState().selectPage(["c"]);
    expect(useSelectionStore.getState().ids().sort()).toEqual(["a", "b", "c"]);
  });

  it("取消当前页选中不影响其它页的选中项", () => {
    useSelectionStore.getState().selectPage(["a", "b", "c"]);
    useSelectionStore.getState().clearPage(["a", "b"]);
    expect(useSelectionStore.getState().ids()).toEqual(["c"]);
  });

  it("重复选中同一 id 不会产生重复项", () => {
    useSelectionStore.getState().selectPage(["a", "a", "a"]);
    useSelectionStore.getState().toggle("a");
    useSelectionStore.getState().toggle("a");
    expect(useSelectionStore.getState().count()).toBe(1);
  });

  it("clear 清空全部", () => {
    useSelectionStore.getState().selectPage(["a", "b"]);
    useSelectionStore.getState().clear();
    expect(useSelectionStore.getState().count()).toBe(0);
  });
});
