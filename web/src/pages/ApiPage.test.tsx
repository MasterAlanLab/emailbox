import { act, fireEvent, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { apiKeyApi, type ApiKey } from "@/api/apiKey";
import { useTenantStore } from "@/store/tenantStore";
import ApiPage from "./ApiPage";

// 这一页守两件事：明文默认不摆在屏幕上，以及「重置」真的换掉了那把 Key。
// 前者是肩窥与截图外泄的第一道门，后者是 Key 泄露之后唯一的补救手段。

const OLD: ApiKey = {
  token: "ebx_1111111111111111111111111111111111111111111111111111111111110000",
  created_at: "2026-08-27T00:00:00Z",
  updated_at: "2026-08-27T00:00:00Z",
};
const NEW: ApiKey = {
  ...OLD,
  token: "ebx_2222222222222222222222222222222222222222222222222222222222229999",
};

function mockGet(key: ApiKey | null) {
  vi.spyOn(apiKeyApi, "get").mockResolvedValue({ code: 0, message: "", data: key });
}

async function mount() {
  const router = createMemoryRouter([{ path: "/", element: <ApiPage /> }], {
    initialEntries: ["/"],
  });
  await act(async () => {
    render(<RouterProvider router={router} />);
  });
}

describe("ApiPage", () => {
  beforeEach(() => {
    useTenantStore.setState({
      activeTenant: {
        id: "t1",
        name: "T",
        slug: "t",
        created_by: "u1",
        created_at: "",
        updated_at: "",
      },
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("默认打码，点「显示」才露出明文", async () => {
    mockGet(OLD);
    await mount();

    const header = screen.getByTestId("api-key-header");
    expect(header.textContent).not.toContain(OLD.token);
    // 但要留出足够的特征让人认出「页面上这把和脚本里配的是同一把」。
    expect(header.textContent).toContain(OLD.token.slice(-4));

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "显示" }));
    });
    expect(screen.getByTestId("api-key-header").textContent).toContain(OLD.token);
  });

  it("重置要二次确认，确认后展示新 Key", async () => {
    mockGet(OLD);
    const reset = vi
      .spyOn(apiKeyApi, "reset")
      .mockResolvedValue({ code: 0, message: "", data: NEW });
    await mount();

    // 直接点「重置」不发请求：这一步要防的是手滑。
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "重置" }));
    });
    expect(reset).not.toHaveBeenCalled();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "确认重置" }));
    });
    expect(reset).toHaveBeenCalledWith("t1");
    // 刚生成的 Key 直接亮出来，用户这一刻就是来拿它的。
    expect(screen.getByTestId("api-key-header").textContent).toContain(NEW.token);
  });

  it("还没生成时显示「生成」而不是「重置」", async () => {
    mockGet(null);
    await mount();

    expect(screen.getByRole("button", { name: "生成 API Key" })).toBeTruthy();
    expect(screen.queryByRole("button", { name: "重置" })).toBeNull();
    expect(screen.queryByTestId("api-key-header")).toBeNull();
  });
});
