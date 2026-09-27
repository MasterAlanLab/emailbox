import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { adminApi, type Plan, type TenantPlan } from "@/api/admin";
import { AssignPlanDialog } from "./AssignPlanDialog";

const plan = (id: string, name: string, extra: Partial<Plan> = {}): Plan => ({
  id,
  code: id,
  name,
  is_default: false,
  max_accounts: 50,
  max_groups: 20,
  daily_mail_fetch: 1000,
  created_at: "",
  updated_at: "",
  ...extra,
});

function mockData(subscription: TenantPlan["subscription"] = null) {
  vi.spyOn(adminApi, "plans").mockResolvedValue({
    code: 0,
    message: "",
    data: [
      plan("free", "免费版", { is_default: true }),
      plan("pro", "专业版", { max_accounts: -1 }),
    ],
  });
  vi.spyOn(adminApi, "tenantPlan").mockResolvedValue({
    code: 0,
    message: "",
    data: {
      limits: {
        plan_id: "free",
        plan_source: "admin",
        plan_code: "free",
        plan_name: "免费版",
        max_accounts: 50,
        max_groups: 20,
        daily_mail_fetch: 1000,
      },
      subscription,
    },
  });
}

async function mount(onAssigned = vi.fn()) {
  await act(async () => {
    render(
      <AssignPlanDialog
        target={{ tenantID: "t1", title: "alice", subtitle: "alice@example.com" }}
        onClose={() => {}}
        onAssigned={onAssigned}
      />,
    );
  });
  return onAssigned;
}

describe("分配套餐", () => {
  afterEach(() => vi.restoreAllMocks());

  // 旧弹窗用 Select.Option 子元素渲染选项，Kumo 的 Select 只认 items，列表整个是空的。
  it("列出全部套餐及其额度，当前套餐不能重复分配", async () => {
    mockData();
    await mount();
    expect(screen.getAllByRole("radio")).toHaveLength(2);
    expect(screen.getByText(/邮箱 不限/)).toBeTruthy();
    expect(screen.getByRole("radio", { name: /免费版/ }).getAttribute("aria-checked")).toBe("true");
    expect(screen.getByRole("button", { name: "分配" }).hasAttribute("disabled")).toBe(true);
  });

  it("选择另一个套餐后直接分配，不需要填写原因", async () => {
    mockData();
    const assign = vi
      .spyOn(adminApi, "assignPlan")
      .mockResolvedValue({ code: 0, message: "", data: {} as TenantPlan });
    const onAssigned = await mount();
    fireEvent.click(screen.getByRole("radio", { name: /专业版/ }));
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "分配" }));
    });
    expect(assign).toHaveBeenCalledWith("t1", "pro");
    expect(onAssigned).toHaveBeenCalled();
  });

  // 手动分配不会停掉用户在 Waffo 的订阅，管理员必须在分配前知道这一点。
  it("用户有付费订阅时提醒订阅仍会扣费", async () => {
    mockData({
      id: "s1",
      plan_id: "pro",
      plan_price_id: "p1",
      status: "active",
      currency: "USD",
      amount: "9.90",
      current_period_end: "2026-10-27",
      cancel_at_period_end: false,
    });
    await mount();
    expect(screen.getByText(/订阅不会被取消、仍会按期扣费/)).toBeTruthy();
  });
});
