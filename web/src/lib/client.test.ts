import { describe, expect, it } from "vitest";
import { CODE_UPSTREAM_MAIL_ERR, shouldClearSession } from "./client";

// 这条边界真的出过事：托管邮箱的 token 失效时后端回 401 + code 1005，
// 拦截器不加区分就会把**用户本人**登出。
describe("shouldClearSession", () => {
  it("本人会话过期时清会话", () => {
    expect(shouldClearSession(401)).toBe(true);
    expect(shouldClearSession(401, 1)).toBe(true);
  });

  it("托管邮箱的上游认证失败不动本人会话", () => {
    expect(shouldClearSession(401, CODE_UPSTREAM_MAIL_ERR)).toBe(false);
  });

  it("其他状态码一概不动会话", () => {
    for (const status of [200, 403, 409, 429, 502]) {
      expect(shouldClearSession(status)).toBe(false);
    }
  });
});
