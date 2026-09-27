import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { adminApi } from "@/api/admin";
import AdminAuditPage from "./AdminAuditPage";

describe("审计日志", () => {
  afterEach(() => vi.restoreAllMocks());

  it("动作显示为中文，详情摊成可读的字段", async () => {
    vi.spyOn(adminApi, "audit").mockResolvedValue({
      code: 0,
      message: "",
      data: {
        items: [
          {
            id: "1",
            tenant_id: "t1",
            actor_user_id: "u1",
            actor_name: "admin",
            actor_kind: "admin",
            action: "plan.assign",
            resource_type: "tenant",
            resource_id: "t1",
            ip: "127.0.0.1",
            details: '{"from_plan":"free","to_plan":"pro"}',
            created_at: "2026-09-27T03:00:00Z",
          },
        ],
        pagination: { page: 1, limit: 50, total: 1, pages: 1 },
      },
    });
    await act(async () => {
      render(<AdminAuditPage />);
    });
    expect(screen.getByText("为用户分配套餐")).toBeTruthy();
    expect(screen.getByText("原套餐：free，新套餐：pro")).toBeTruthy();
    expect(screen.queryByText("plan.assign")).toBeNull();
  });
});
