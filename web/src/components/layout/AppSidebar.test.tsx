import { render, screen } from "@testing-library/react";
import { MemoryRouter } from "react-router-dom";
import { afterEach, describe, expect, it } from "vitest";
import type { User } from "@/api/user";
import { useAuthStore } from "@/store/authStore";
import { AppSidebar } from "./AppSidebar";

function renderAs(role: "admin" | "user") {
  useAuthStore.setState({
    user: { id: "u1", username: "alice", email: "", platform_role: role } as User,
  });
  return render(
    <MemoryRouter initialEntries={["/mail"]}>
      <AppSidebar />
    </MemoryRouter>,
  );
}

describe("主导航的管理入口", () => {
  afterEach(() => useAuthStore.setState({ user: null }));

  // 管理功能各自是一个入口，不再藏在一个带页签的「后台」后面。
  it("管理员看到管理分组下的四个独立入口", () => {
    renderAs("admin");
    const group = screen.getByRole("group", { name: "管理" });
    const links = Array.from(group.querySelectorAll("a")).map((a) => [
      a.textContent,
      a.getAttribute("href"),
    ]);
    expect(links).toEqual([
      ["概览", "/admin"],
      ["用户", "/admin/users"],
      ["套餐", "/admin/plans"],
      ["审计", "/admin/audit"],
    ]);
    expect(screen.queryByText("后台")).toBeNull();
  });

  it("普通用户看不到管理分组", () => {
    renderAs("user");
    expect(screen.queryByRole("group", { name: "管理" })).toBeNull();
    expect(screen.queryByRole("link", { name: "套餐" })).toBeNull();
  });
});
