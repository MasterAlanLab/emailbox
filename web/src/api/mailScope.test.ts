import { describe, expect, it } from "vitest";
import { asScope, mailBase, mailScopeKey } from "./mail";

// 06 文档 §9 把这一条列为必测项，理由是它错了几乎不会被当场发现：
// 管理员前缀漏掉的话，页面照样正常渲染——只不过显示的是管理员**自己**的邮箱，
// 而他以为自己在看别人的。反过来多加了前缀，普通用户会直接 403。
describe("mailBase · scope 切换", () => {
  it("裸 tenantID 默认走用户视图", () => {
    expect(mailBase("t-1")).toBe("/api/v1/tenants/t-1/mail");
  });

  it("admin scope 走管理员前缀，且带上目标租户", () => {
    expect(mailBase({ tenantID: "t-2", admin: true })).toBe("/api/v1/admin/tenants/t-2/mail");
  });

  it("admin=false 与不传 admin 等价", () => {
    expect(mailBase({ tenantID: "t-3", admin: false })).toBe(mailBase("t-3"));
    expect(mailBase({ tenantID: "t-3" })).toBe(mailBase("t-3"));
  });

  it("管理员视图绝不会落到用户自己的租户路径上", () => {
    const path = mailBase({ tenantID: "victim", admin: true });
    expect(path.startsWith("/api/v1/admin/")).toBe(true);
    expect(path).toContain("victim");
  });
});

describe("mailScopeKey", () => {
  // 这个键用来拼组件里的重置键。两种 scope 必须给出不同的值，
  // 否则管理员从自己的邮箱切到别人的邮箱时列表不会重置。
  it("区分用户视图与管理员视图", () => {
    expect(mailScopeKey("t-1")).not.toBe(mailScopeKey({ tenantID: "t-1", admin: true }));
  });

  it("永远是字符串，不会退化成 [object Object]", () => {
    const key = mailScopeKey({ tenantID: "t-1", admin: true });
    expect(typeof key).toBe("string");
    expect(key).not.toContain("[object");
  });
});

describe("asScope", () => {
  it("把裸字符串补成 scope 对象", () => {
    expect(asScope("t-9")).toEqual({ tenantID: "t-9" });
  });

  it("原样返回已经是对象的 scope", () => {
    const scope = { tenantID: "t-9", admin: true };
    expect(asScope(scope)).toBe(scope);
  });
});
