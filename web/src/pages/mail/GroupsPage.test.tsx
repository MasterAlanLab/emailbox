import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mailApi, type Limits, type MailGroupNode } from "@/api/mail";
import { tenantApi } from "@/api/tenant";
import { useTenantStore } from "@/store/tenantStore";
import GroupsPage from "./GroupsPage";

// 这一页真正要守的是那几条禁用规则。每一条都对应一个后端必定拒绝的操作——
// 前端不挡，用户就只能靠撞一次报错来知道规则，而报错文案里说不清「为什么」。

function group(id: string, name: string, extra: Partial<MailGroupNode> = {}): MailGroupNode {
  return {
    id,
    name,
    description: "",
    color: "gray",
    sort_order: 0,
    is_system: false,
    created_at: "",
    updated_at: "",
    proxy_url_masked: "",
    fallback_proxy_url_1_masked: "",
    fallback_proxy_url_2_masked: "",
    account_count: 0,
    ...extra,
  };
}

const LIMITS: Limits = {
  plan_code: "free",
  plan_name: "免费版",
  max_accounts: 50,
  max_groups: 20,
  daily_mail_fetch: 1000,
};

function mockGroups(groups: MailGroupNode[]) {
  vi.spyOn(mailApi, "groups").mockResolvedValue({ code: 0, message: "", data: groups });
}

function mockQuota(maxGroups: number, used: number) {
  vi.spyOn(tenantApi, "quota").mockResolvedValue({
    code: 0,
    message: "",
    data: {
      limits: { ...LIMITS, max_groups: maxGroups },
      usage: { accounts: 0, groups: used, mail_fetch: 0, token_refresh: 0 },
      day: "2026-08-27",
    },
  });
}

// 挂载包一层 act：base-ui 的 Menu 在挂载后还会异步定位一次弹层，
// 不包的话每个用例都会打一串 "not wrapped in act(...)"，把真正的失败淹掉。
async function mount() {
  const router = createMemoryRouter([{ path: "/", element: <GroupsPage /> }], {
    initialEntries: ["/"],
  });
  await act(async () => {
    render(<RouterProvider router={router} />);
  });
}

// 打开某一行的 ⋯ 菜单。用 DOM 直查而不是 getAllByRole：base-ui 把菜单渲染到
// portal 里并挂了几层 focus guard，可访问性树上偶尔漏掉第一项。
async function openMenu(groupName: string) {
  const trigger = await screen.findByRole("button", { name: `${groupName} 更多操作` });
  await act(async () => {
    fireEvent.click(trigger);
  });
  return Array.from(document.querySelectorAll('[role="menuitem"]'));
}

const item = (items: Element[], label: string) =>
  items.find((i) => i.textContent?.trim() === label)!;

const isDisabled = (el: Element) => el.getAttribute("aria-disabled") === "true";

describe("GroupsPage", () => {
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
    mockQuota(20, 1);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("系统分组不能删除，但可以改名改色", async () => {
    mockGroups([group("sys", "默认分组", { is_system: true })]);
    await mount();

    const items = await openMenu("默认分组");
    // 它是所有账号的回落目标（GroupService.Delete 拿它做 fallback），
    // 没了之后删任何一个分组都会失败。
    expect(isDisabled(item(items, "删除"))).toBe(true);
    // 但别把整行锁死——改名改色是安全的。
    expect(isDisabled(item(items, "编辑"))).toBe(false);
  });

  it("只有一个分组时上移下移都禁用，不发一次没意义的重排请求", async () => {
    mockGroups([group("a", "唯一")]);
    await mount();

    const items = await openMenu("唯一");
    expect(isDisabled(item(items, "上移"))).toBe(true);
    expect(isDisabled(item(items, "下移"))).toBe(true);
  });

  it("首尾两项各自只禁用一个方向", async () => {
    mockGroups([group("a", "第一"), group("b", "第二"), group("c", "第三")]);
    await mount();

    const first = await openMenu("第一");
    expect(isDisabled(item(first, "上移"))).toBe(true);
    expect(isDisabled(item(first, "下移"))).toBe(false);
  });

  it("达到套餐上限时禁用「新建分组」，而不是让用户填完表单再撞 1001", async () => {
    mockQuota(1, 1);
    mockGroups([group("sys", "默认分组", { is_system: true })]);
    await mount();

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "新建分组" })).toHaveProperty("disabled", true),
    );
    expect(screen.getByText("1 / 1 个分组")).toBeTruthy();
  });

  it("配额接口挂了也照常显示分组列表", async () => {
    // 少一行「12 / 20」，远不如「分组明明取到了却显示加载失败」严重。
    vi.spyOn(tenantApi, "quota").mockRejectedValue(new Error("配额服务不可用"));
    mockGroups([group("a", "客户 A")]);
    await mount();

    expect(await screen.findByText("客户 A")).toBeTruthy();
    expect(screen.queryByText("配额服务不可用")).toBeNull();
    // 上限未知时不能装作没有上限地显示「1 / 0」。
    expect(screen.getByText("1 个分组，套餐不限数量")).toBeTruthy();
  });

  it("重排发的是整组 ID 顺序，而不只是被移动的那一个", async () => {
    mockGroups([group("a", "第一"), group("b", "第二"), group("c", "第三")]);
    const reorder = vi
      .spyOn(mailApi, "reorderGroups")
      .mockResolvedValue({ code: 0, message: "", data: null });
    await mount();

    const items = await openMenu("第三");
    await act(async () => {
      fireEvent.click(item(items, "上移"));
    });

    await waitFor(() => expect(reorder).toHaveBeenCalledWith("t1", ["a", "c", "b"]));
  });
});
