import { act, render, screen, within } from "@testing-library/react";
import { createMemoryRouter, MemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { useAuthStore } from "@/store/authStore";
import { appRoute, shellRoute } from "@/router/handle";
import { Layout } from "./Layout";
import { PublicHeader } from "./PublicHeader";

const initialAuthState = useAuthStore.getState();

beforeEach(() => {
  useAuthStore.setState({ user: null, isAuthenticated: false, loading: false, desktop: false });
});

afterEach(() => {
  useAuthStore.setState(initialAuthState);
});

// Layout 靠路由 handle 在三种形态之间切换。最容易出错的是**漏给某个页面挂 handle**：
// 症状是那个页面突然没了左侧导航栏、还冒出一个顶栏，很容易被当成样式问题去查 CSS，
// 其实是路由没配。三个方向都钉住，任何一边退化都会立刻失败。

function renderAt(path: string) {
  const router = createMemoryRouter(
    [
      {
        path: "/",
        element: <Layout />,
        children: [
          { path: "public", element: <p>公开页面</p> },
          { path: "settings", element: <p>设置页面</p>, handle: appRoute },
          { path: "mail", element: <p>工作台</p>, handle: shellRoute },
        ],
      },
    ],
    { initialEntries: [path] },
  );
  return render(<RouterProvider router={router} />);
}

describe("Layout 的三种形态", () => {
  it("公开页有顶栏和页脚，整页可滚", () => {
    const { container } = renderAt("/public");
    const root = container.firstElementChild!;

    expect(root.className).toContain("min-h-screen");
    expect(screen.getByText("隐私政策")).toBeTruthy();
    expect(
      within(screen.getByRole("contentinfo"))
        .getByRole("link", { name: "contact@mail.corouter.cc" })
        .getAttribute("href"),
    ).toBe("mailto:contact@mail.corouter.cc");
    // 访客身上没有导航栏，登录入口必须摆在顶上。
    expect(screen.getByRole("link", { name: "登录" })).toBeTruthy();
    expect(screen.queryByRole("navigation", { name: "主导航" })).toBeNull();
  });

  it("应用页有侧边导航栏和客服页脚、没有顶栏，内容区自己滚", () => {
    const { container } = renderAt("/settings");
    const root = container.firstElementChild!;

    expect(root.className).toContain("h-dvh");
    expect(screen.getByRole("navigation", { name: "主导航" })).toBeTruthy();
    // 所有入口共用一份间距，令牌和用量之间也没有额外的分组占位。
    const tokens = screen.getByRole("link", { name: "令牌" });
    expect(tokens.nextElementSibling).toBe(screen.getByRole("link", { name: "用量" }));
    // 公开页的完整导航不会挤进应用页，客服联系方式始终留在内容区外。
    expect(screen.queryByText("隐私政策")).toBeNull();
    expect(screen.queryByRole("link", { name: "注册" })).toBeNull();
    const main = container.querySelector("main")!;
    const footer = screen.getByRole("contentinfo");
    expect(
      within(footer).getByRole("link", { name: "contact@mail.corouter.cc" }).getAttribute("href"),
    ).toBe("mailto:contact@mail.corouter.cc");
    expect(main.nextElementSibling).toBe(footer);
    expect(main.className).toContain("overflow-y-auto");
    expect(main.className).toContain("min-h-0");
    expect(footer.className).toContain("shrink-0");
  });

  it("工作台页在应用页基础上再关掉内容区自身的滚动", () => {
    const { container } = renderAt("/mail");

    expect(screen.getByRole("navigation", { name: "主导航" })).toBeTruthy();
    // /mail 的滚动由它内部各面板分别负责，内容区自己一滚，
    // 工具条和状态栏就会跟着跑出视口。
    const main = container.querySelector("main")!;
    const footer = screen.getByRole("contentinfo");
    expect(
      within(footer).getByRole("link", { name: "contact@mail.corouter.cc" }).getAttribute("href"),
    ).toBe("mailto:contact@mail.corouter.cc");
    expect(main.nextElementSibling).toBe(footer);
    expect(main.className).not.toContain("overflow-y-auto");
    expect(main.className).toContain("overflow-hidden");
    expect(main.className).toContain("min-h-0");
  });
});

it("公开导航等会话恢复完成后直接显示控制台入口", () => {
  useAuthStore.setState({ loading: true });
  render(
    <MemoryRouter>
      <PublicHeader />
    </MemoryRouter>,
  );

  expect(screen.queryByRole("link", { name: "登录" })).toBeNull();
  expect(screen.queryByRole("link", { name: "注册" })).toBeNull();
  act(() => useAuthStore.setState({ isAuthenticated: true, loading: false }));
  expect(screen.getByRole("link", { name: "进入控制台" }).getAttribute("href")).toBe("/mail");
  expect(screen.queryByRole("link", { name: "登录" })).toBeNull();
  expect(screen.queryByRole("link", { name: "注册" })).toBeNull();
});

// 桌面版的本地账号是首次启动时用随机密码建的，密码从不展示也从不留存。
// 退出之后停在登录页，用户填不出任何凭据，只能重启应用——所以那个入口在
// 桌面形态下必须不存在。这不是样式问题，钉住的是「点了会把人锁在外面」。
describe("退出入口的形态差异", () => {
  it("网页形态保留退出入口", () => {
    useAuthStore.setState({ desktop: false });
    renderAt("/settings");
    expect(screen.getByRole("button", { name: "退出" })).toBeTruthy();
  });

  it("桌面形态藏掉退出入口", () => {
    useAuthStore.setState({ desktop: true });
    renderAt("/settings");
    expect(screen.queryByRole("button", { name: "退出" })).toBeNull();
    // 同时确认导航栏本身渲染正常：少了这条，整个侧边栏崩掉时上面那句也是绿的。
    expect(screen.getByRole("link", { name: "令牌" })).toBeTruthy();
  });
});
