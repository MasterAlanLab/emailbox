import { render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { describe, expect, it } from "vitest";
import { appRoute, shellRoute } from "@/router/handle";
import { Layout } from "./Layout";

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
    // 访客身上没有导航栏，登录入口必须摆在顶上。
    expect(screen.getByRole("link", { name: "登录" })).toBeTruthy();
    expect(screen.queryByRole("navigation", { name: "主导航" })).toBeNull();
  });

  it("应用页有侧边导航栏、没有顶栏和页脚，内容区自己滚", () => {
    const { container } = renderAt("/settings");
    const root = container.firstElementChild!;

    expect(root.className).toContain("h-dvh");
    expect(screen.getByRole("navigation", { name: "主导航" })).toBeTruthy();
    // 顶栏的两个标志物都不该出现——所有全局入口都在侧边栏里。
    expect(screen.queryByText("隐私政策")).toBeNull();
    expect(screen.queryByRole("link", { name: "注册" })).toBeNull();
    expect(container.querySelector("main")!.className).toContain("overflow-y-auto");
  });

  it("工作台页在应用页基础上再关掉内容区自身的滚动", () => {
    const { container } = renderAt("/mail");

    expect(screen.getByRole("navigation", { name: "主导航" })).toBeTruthy();
    // /mail 的滚动由它内部各面板分别负责，内容区自己一滚，
    // 工具条和状态栏就会跟着跑出视口。
    expect(container.querySelector("main")!.className).not.toContain("overflow-y-auto");
  });
});
