import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { mailApi, type MailGroup, type MailGroupNode } from "@/api/mail";
import { GroupFormDialog } from "./GroupFormDialog";

// 这里只测代理那一段。名称、描述是普通的受控输入，出错也就是存错一个字；
// 代理不是——它决定这批账号从哪个 IP 出站，错了的表现是「静默直连」，
// 界面上看不出来，等发现时真实地址已经暴露过了。
//
// 守的是两条：编辑时必须拿明文回填（回填打码串会让用户把 "****" 存回库里），
// 以及明文没到手时保存必须省掉代理字段（照发一个空串等于清空）。

function group(extra: Partial<MailGroupNode> = {}): MailGroupNode {
  return {
    id: "g1",
    name: "客户 A",
    description: "",
    color: "gray",
    sort_order: 0,
    is_system: false,
    created_at: "",
    updated_at: "",
    proxy_url_masked: "socks5://puser:****@proxy.example.com:1080",
    account_count: 0,
    ...extra,
  };
}

const PLAIN = "socks5://puser:psecret@proxy.example.com:1080";

function mockProxy(data: { proxy_url: string }) {
  return vi.spyOn(mailApi, "groupProxy").mockResolvedValue({
    code: 0,
    message: "",
    data: { proxy_url: data.proxy_url },
  });
}

function mockUpdate() {
  return vi
    .spyOn(mailApi, "updateGroup")
    .mockResolvedValue({ code: 0, message: "", data: {} as MailGroup });
}

async function mount(g?: MailGroupNode) {
  await act(async () => {
    render(<GroupFormDialog tenantID="t1" group={g} onClose={() => {}} onSaved={() => {}} />);
  });
}

async function save(label: string) {
  await act(async () => {
    fireEvent.click(screen.getByRole("button", { name: label }));
  });
}

describe("GroupFormDialog 的代理配置", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("编辑时回填明文，保存原样发回去", async () => {
    mockProxy({ proxy_url: PLAIN });
    const update = mockUpdate();

    await mount(group());

    // 回填的是明文而不是列表里那个打码串。
    await screen.findByDisplayValue(PLAIN);
    expect(screen.queryByDisplayValue(/\*\*\*\*/)).toBeNull();

    await save("保存");
    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update.mock.calls[0][2]).toMatchObject({ proxy_url: PLAIN });
  });

  it("明文没取到时，保存不带代理字段——空输入框不能被当成「清空」", async () => {
    vi.spyOn(mailApi, "groupProxy").mockRejectedValue(new Error("代理地址解密失败"));
    const update = mockUpdate();

    await mount(group());
    await screen.findByText(/代理读取失败/);

    // 名称照常能改，改的是名称就只发名称。
    fireEvent.change(screen.getByDisplayValue("客户 A"), { target: { value: "客户 B" } });
    await save("保存");

    await waitFor(() => expect(update).toHaveBeenCalled());
    const payload = update.mock.calls[0][2];
    expect(payload).toMatchObject({ name: "客户 B" });
    expect(payload).not.toHaveProperty("proxy_url");
  });

  it("新建分组不去读代理明文", async () => {
    const proxy = mockProxy({ proxy_url: "" });
    await mount();
    // 那个端点每调一次就写一条审计。新建时没有分组可读，一次都不该发。
    expect(proxy).not.toHaveBeenCalled();
  });
});
