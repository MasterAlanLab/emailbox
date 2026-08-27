import { describe, expect, it } from "vitest";
import type { MailGroupNode } from "@/api/mail";
import { findSystemGroup, groupSelectItems } from "./groupOptions";

function group(id: string, name: string, isSystem = false, accountCount = 1): MailGroupNode {
  return {
    id,
    name,
    description: "",
    color: "gray",
    sort_order: 0,
    is_system: isSystem,
    created_at: "",
    updated_at: "",
    proxy_url_masked: "",
    fallback_proxy_url_1_masked: "",
    fallback_proxy_url_2_masked: "",
    account_count: accountCount,
  };
}

describe("groupSelectItems", () => {
  it("不传 allLabel 时不插入空值项", () => {
    const items = groupSelectItems([group("a", "A")]);
    expect(items).toEqual([{ label: "A", value: "a" }]);
  });

  // 导入页此前硬编码了一个 value="" 的「默认分组」占位项，而系统分组的名字
  // 也叫「默认分组」，于是下拉里出现两个同名选项，指向的却是同一个分组。
  it("系统分组只出现一次，且不产生空值项", () => {
    const items = groupSelectItems([group("sys", "默认分组", true)]);
    expect(items.filter((i) => i.label === "默认分组")).toHaveLength(1);
    expect(items.some((i) => i.value === "")).toBe(false);
  });

  it("allLabel 排在最前面且值为空串", () => {
    const items = groupSelectItems([group("a", "A")], { allLabel: "全部分组" });
    expect(items[0]).toEqual({ label: "全部分组", value: "" });
  });

  it("counts 打开时缀上账号数", () => {
    const items = groupSelectItems([group("a", "A", false, 3)], { counts: true });
    expect(items[0].label).toBe("A (3)");
  });
});

describe("findSystemGroup", () => {
  it("找出系统分组", () => {
    expect(findSystemGroup([group("a", "A"), group("sys", "默认分组", true)])?.id).toBe("sys");
  });

  it("没有系统分组时返回 undefined，调用方据此退回空串", () => {
    expect(findSystemGroup([group("a", "A")])).toBeUndefined();
  });
});
