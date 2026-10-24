import { act, fireEvent, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, describe, expect, it, vi } from "vitest";
import { pricingApi } from "@/api/pricing";
import PricingPage from "./PricingPage";

describe("公开定价", () => {
  afterEach(() => vi.restoreAllMocks());

  // 加载失败不能被解释成「当前免费」：访客必须等到真实额度与价格返回后再作判断。
  it("加载和失败时不显示套餐，重试后展示服务端额度及完整年费", async () => {
    let reject!: (error: Error) => void;
    const get = vi.spyOn(pricingApi, "get").mockImplementationOnce(
      () =>
        new Promise((_, fail) => {
          reject = fail;
        }),
    );
    const router = createMemoryRouter([{ path: "/pricing", element: <PricingPage /> }], {
      initialEntries: ["/pricing"],
    });
    render(<RouterProvider router={router} />);
    expect(screen.getByRole("status")).toBeTruthy();
    expect(screen.queryByText("免费")).toBeNull();

    await act(async () => reject(new Error("offline")));
    expect(screen.getByRole("alert")).toBeTruthy();
    expect(screen.queryByText("免费")).toBeNull();

    get.mockResolvedValueOnce({
      code: 0,
      message: "",
      data: {
        billing_enabled: true,
        free_plan: { name: "入门", max_accounts: 37, max_groups: -1, daily_mail_fetch: 823 },
        paid_plans: [
          {
            name: "进阶",
            max_accounts: 250,
            max_groups: 20,
            daily_mail_fetch: 5000,
            prices: [
              { billing_period: "monthly", currency: "USD", amount: "9.90" },
              { billing_period: "yearly", currency: "USD", amount: "99.00" },
            ],
          },
        ],
      },
    });
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "重新加载" })));

    expect(get).toHaveBeenCalledTimes(2);
    expect(screen.getByText("37 个")).toBeTruthy();
    expect(screen.getByText("823 次")).toBeTruthy();
    expect(screen.getByText("不限")).toBeTruthy();
    expect(screen.getByText("9.90")).toBeTruthy();
    expect(screen.getByText("USD / 月")).toBeTruthy();
    expect(screen.getByText("99.00")).toBeTruthy();
    expect(screen.getByText("USD / 年")).toBeTruthy();
  });
});
