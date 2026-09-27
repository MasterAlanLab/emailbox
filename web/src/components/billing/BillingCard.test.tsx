import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { tenantApi, type BillingSubscription } from "@/api/tenant";
import { BillingCard } from "./BillingCard";

const price = {
  id: "price-1",
  plan_id: "plan-pro",
  plan_code: "pro",
  plan_name: "专业版",
  billing_period: "monthly" as const,
  currency: "USD",
  amount: "9.90",
  sync_status: "active" as const,
  active: true,
};

function mockBilling(subscription: BillingSubscription | null) {
  vi.spyOn(tenantApi, "billingPlans").mockResolvedValue({ code: 0, message: "", data: [price] });
  vi.spyOn(tenantApi, "syncBilling").mockResolvedValue({
    code: 0,
    message: "",
    data: { subscription, changed: false },
  });
}

async function mount(navigate = vi.fn()) {
  await act(async () => {
    render(
      <BillingCard
        tenantID="t1"
        reloadKey={0}
        returnedFromCheckout={false}
        onRefresh={() => {}}
        navigate={navigate}
      />,
    );
  });
  return navigate;
}

describe("订阅卡片", () => {
  afterEach(() => vi.restoreAllMocks());

  // 已结束的订阅不能挡住重新购买：取消后再订阅是最常见的回流路径。
  it("订阅已结束时仍可购买，结账地址原样用于跳转", async () => {
    mockBilling({
      id: "s1",
      plan_id: "plan-pro",
      plan_price_id: "price-1",
      status: "canceled",
      currency: "USD",
      amount: "9.90",
      cancel_at_period_end: false,
    });
    const checkout = vi.spyOn(tenantApi, "checkout").mockResolvedValue({
      code: 0,
      message: "",
      data: { checkout_url: "https://checkout.waffo.test/s#token=abc", checkout_id: "c1" },
    });
    const navigate = await mount();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "订阅" }));
    });
    expect(checkout).toHaveBeenCalledWith(
      "t1",
      expect.objectContaining({ plan_price_id: "price-1" }),
    );
    expect(navigate).toHaveBeenCalledWith("https://checkout.waffo.test/s#token=abc");
  });

  it("生效中的订阅不显示购买按钮，只给到期取消", async () => {
    mockBilling({
      id: "s1",
      plan_id: "plan-pro",
      plan_price_id: "price-1",
      status: "active",
      currency: "USD",
      amount: "9.90",
      current_period_end: "2026-10-27",
      cancel_at_period_end: false,
    });
    await mount();
    expect(screen.queryByRole("button", { name: "订阅" })).toBeNull();
    expect(screen.getByText("生效中")).toBeTruthy();
    expect(screen.getByRole("button", { name: "到期后取消" })).toBeTruthy();
  });

  // 对账发现套餐变了（Webhook 没到、靠对账入的账）：让用量页重取配额。
  it("对账改变了订阅时通知页面刷新配额", async () => {
    vi.spyOn(tenantApi, "billingPlans").mockResolvedValue({ code: 0, message: "", data: [price] });
    vi.spyOn(tenantApi, "syncBilling").mockResolvedValue({
      code: 0,
      message: "",
      data: { subscription: null, changed: true },
    });
    const onRefresh = vi.fn();
    await act(async () => {
      render(
        <BillingCard
          tenantID="t1"
          reloadKey={0}
          returnedFromCheckout={false}
          onRefresh={onRefresh}
        />,
      );
    });
    expect(onRefresh).toHaveBeenCalledTimes(1);
  });
});
