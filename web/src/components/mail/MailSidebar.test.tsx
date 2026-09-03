import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { mailApi, type Limits, type MailGroupNode } from "@/api/mail";
import { tenantApi } from "@/api/tenant";
import { MailSidebar } from "./MailSidebar";

// 分组管理原本是独立的一页（/mail/groups），现在收进了邮箱工作台的左栏。
// 页面没了，但它真正要守的那几条禁用规则得跟着搬过来——每一条都对应一个
// 后端必定拒绝的操作，前端不挡，用户就只能靠撞一次报错来知道规则，
// 而报错文案里说不清「为什么」。

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
    refresh_interval_minutes: 0,
    next_refresh_at: null,
    proxy_url_masked: "",
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

function mockQuota(maxGroups: number) {
  vi.spyOn(tenantApi, "quota").mockResolvedValue({
    code: 0,
    message: "",
    data: {
      limits: { ...LIMITS, max_groups: maxGroups },
      usage: { accounts: 0, groups: 0, mail_fetch: 0, token_refresh: 0 },
      day: "2026-08-27",
    },
  });
}

// 挂载包一层 act：base-ui 的 Menu 在挂载后还会异步定位一次弹层，
// 不包的话每个用例都会打一串 "not wrapped in act(...)"，把真正的失败淹掉。
async function mount(groups: MailGroupNode[], onGroupsChanged = () => {}) {
  const element = (
    <MailSidebar
      tenantID="t1"
      groups={groups}
      groupID={null}
      onGroupChange={() => {}}
      refreshStatus=""
      onRefreshStatusChange={() => {}}
      stats={null}
      onGroupsChanged={onGroupsChanged}
    />
  );
  const router = createMemoryRouter([{ path: "/", element }], { initialEntries: ["/"] });
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

describe("MailSidebar 的分组管理", () => {
  beforeEach(() => {
    // 折叠状态存在 localStorage 里，不清会从上一个用例漏过来，
    // 表现为「分组段整个不见了」这种莫名其妙的失败。
    localStorage.clear();
    mockQuota(20);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("系统分组不能删除，但可以改名改色", async () => {
    await mount([group("sys", "默认分组", { is_system: true })]);

    const items = await openMenu("默认分组");
    // 它是所有账号的回落目标（GroupService.Delete 拿它做 fallback），
    // 没了之后删任何一个分组都会失败。
    expect(isDisabled(item(items, "删除"))).toBe(true);
    // 但别把整行锁死——改名改色是安全的。
    expect(isDisabled(item(items, "编辑"))).toBe(false);
  });

  it("只有一个分组时上移下移都禁用，不发一次没意义的重排请求", async () => {
    await mount([group("a", "唯一")]);

    const items = await openMenu("唯一");
    expect(isDisabled(item(items, "上移"))).toBe(true);
    expect(isDisabled(item(items, "下移"))).toBe(true);
  });

  it("首尾两项各自只禁用一个方向", async () => {
    await mount([group("a", "第一"), group("b", "第二"), group("c", "第三")]);

    const first = await openMenu("第一");
    expect(isDisabled(item(first, "上移"))).toBe(true);
    expect(isDisabled(item(first, "下移"))).toBe(false);
  });

  it("达到套餐上限时禁用「新建分组」，而不是让用户填完表单再撞 1001", async () => {
    mockQuota(1);
    await mount([group("sys", "默认分组", { is_system: true })]);

    await waitFor(() =>
      expect(screen.getByRole("button", { name: "新建分组" })).toHaveProperty("disabled", true),
    );
  });

  it("配额接口挂了也照常显示分组列表，且不把新建按钮误锁成已满", async () => {
    vi.spyOn(tenantApi, "quota").mockRejectedValue(new Error("配额服务不可用"));
    await mount([group("a", "客户 A")]);

    expect(await screen.findByText("客户 A")).toBeTruthy();
    expect(screen.queryByText("配额服务不可用")).toBeNull();
    // 上限未知不等于已满：这里锁死的话，配额接口一挂就再也建不了分组。
    expect(screen.getByRole("button", { name: "新建分组" })).toHaveProperty("disabled", false);
  });

  it("重排发的是整组 ID 顺序，而不只是被移动的那一个", async () => {
    const reorder = vi
      .spyOn(mailApi, "reorderGroups")
      .mockResolvedValue({ code: 0, message: "", data: null });
    await mount([group("a", "第一"), group("b", "第二"), group("c", "第三")]);

    const items = await openMenu("第三");
    await act(async () => {
      fireEvent.click(item(items, "上移"));
    });

    await waitFor(() => expect(reorder).toHaveBeenCalledWith("t1", ["a", "c", "b"]));
  });

  it("管理员看别人的租户时不去要配额——那是成员接口，他不是成员", async () => {
    const quota = vi.spyOn(tenantApi, "quota");
    const element = (
      <MailSidebar
        tenantID={{ tenantID: "t9", admin: true }}
        groups={[group("a", "客户 A")]}
        groupID={null}
        onGroupChange={() => {}}
        refreshStatus=""
        onRefreshStatusChange={() => {}}
        stats={null}
        onGroupsChanged={() => {}}
      />
    );
    const router = createMemoryRouter([{ path: "/", element }], { initialEntries: ["/"] });
    await act(async () => {
      render(<RouterProvider router={router} />);
    });

    expect(quota).not.toHaveBeenCalled();
    // 上限未知，所以按钮照常可用（真超了后端会拦）。
    expect(screen.getByRole("button", { name: "新建分组" })).toHaveProperty("disabled", false);
  });
});

describe("MailSidebar 的折叠", () => {
  beforeEach(() => {
    localStorage.clear();
    mockQuota(20);
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("两段都能收起，且收起状态记得住", async () => {
    await mount([group("a", "客户 A")]);

    // 默认两段都展开。
    expect(screen.getByText("登录失败")).toBeTruthy();
    expect(screen.getByText("客户 A")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /账号状态/ }));
    });
    expect(screen.queryByText("登录失败")).toBeNull();
    // 收起状态段不该连带把分组段也收了。
    expect(screen.getByText("客户 A")).toBeTruthy();

    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: /邮箱分组/ }));
    });
    expect(screen.queryByText("客户 A")).toBeNull();

    expect(localStorage.getItem("emailbox.mail.sidebar.status")).toBe("false");
    expect(localStorage.getItem("emailbox.mail.sidebar.groups")).toBe("false");
  });
});
