import { act, render, screen } from "@testing-library/react";
import { createMemoryRouter, RouterProvider } from "react-router-dom";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { jobApi } from "@/api/jobs";
import { mailApi, type MailAccount } from "@/api/mail";
import { useJobStore } from "@/store/jobStore";
import TokensPage from "./TokensPage";

function account(id: string, errorKind: string, error: string): MailAccount {
  return {
    id,
    group_id: "g1",
    email: `${id}@outlook.com`,
    provider: "outlook",
    account_type: "outlook",
    auth_channel: "imap_new",
    client_id: "client",
    imap_host: "",
    imap_port: 993,
    status: "active",
    remark: "",
    sort_order: 0,
    last_refresh_at: "2026-08-30T00:00:00Z",
    last_refresh_status: "failed",
    last_refresh_error: error,
    last_refresh_error_kind: errorKind,
    created_at: "",
    updated_at: "",
    has_password: false,
    has_refresh_token: true,
    has_imap_password: false,
    proxy_url_masked: "",
    fallback_proxy_url_1_masked: "",
    fallback_proxy_url_2_masked: "",
    aliases: [],
  };
}

async function mount() {
  const router = createMemoryRouter([
    { path: "/", element: <TokensPage scope={{ tenantID: "t1" }} /> },
  ]);
  await act(async () => {
    render(<RouterProvider router={router} />);
  });
}

describe("Token 刷新结果", () => {
  beforeEach(() => {
    useJobStore.getState().reset();
    vi.spyOn(mailApi, "groups").mockResolvedValue({ code: 0, message: "", data: [] });
    vi.spyOn(mailApi, "accounts").mockResolvedValue({
      code: 0,
      message: "",
      data: { items: [], pagination: { page: 1, limit: 200, total: 0, pages: 0 } },
    });
    vi.spyOn(jobApi, "stats").mockResolvedValue({
      code: 0,
      message: "",
      data: {
        total: 3,
        success: 1,
        failed: 2,
        never: 0,
        by_error_kind: { auth_failed: 2 },
        last_job: null,
      },
    });
  });

  afterEach(() => {
    vi.restoreAllMocks();
    useJobStore.getState().reset();
  });

  it("成功结果照常显示，同类认证失败直接展示各自具体原因", async () => {
    const expired = "刷新令牌因长期未使用而过期，请重新授权（AADSTS700082）";
    const login = "Microsoft 要求重新登录并完成多重验证（AADSTS50076）";
    useJobStore.setState({
      jobID: "job1",
      status: "partial",
      progress: { total: 3, success: 1, failed: 2, done: 3, current: "" },
      recent: [
        {
          accountID: "imap",
          email: "imap@outlook.com",
          status: "success",
          errorKind: "",
          error: "",
        },
        {
          accountID: "old",
          email: "old@outlook.com",
          status: "failed",
          errorKind: "auth_failed",
          error: expired,
        },
        {
          accountID: "login",
          email: "login@outlook.com",
          status: "failed",
          errorKind: "auth_failed",
          error: login,
        },
      ],
    });
    await mount();

    expect(screen.getByText("成功")).toBeTruthy();
    expect(screen.getByText(expired)).toBeTruthy();
    expect(screen.getByText(login)).toBeTruthy();
    expect(screen.getAllByText("认证失败")).toHaveLength(3);
    expect(screen.queryByText("令牌失效，需重新授权")).toBeNull();
  });

  it("刷新失败面板保留服务端原因，并按错误类型提供对应动作", async () => {
    const login = "Microsoft 要求重新登录并完成多重验证（AADSTS50076）";
    const consent = "尚未授予所需邮件权限，请重新授权（AADSTS65001）";
    const client = "OAuth 客户端配置错误，请检查 refresh_token 对应的 client_id";
    const accountsSpy = vi.mocked(mailApi.accounts);
    accountsSpy
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          items: [{ ...account("password", "network", "连接失败"), account_type: "imap" }],
          pagination: { page: 1, limit: 200, total: 4, pages: 2 },
        },
      })
      .mockResolvedValueOnce({
        code: 0,
        message: "",
        data: {
          items: [
            account("login", "auth_failed", login),
            account("consent", "consent_required", consent),
            account("client", "provider_error", client),
          ],
          pagination: { page: 2, limit: 200, total: 4, pages: 2 },
        },
      });
    await mount();

    expect(screen.getByText(login)).toBeTruthy();
    expect(screen.getByText(consent)).toBeTruthy();
    expect(screen.getByText(client)).toBeTruthy();
    expect(screen.getByText("权限不足")).toBeTruthy();
    expect(screen.getByText("服务商或应用配置错误")).toBeTruthy();
    expect(screen.queryByText("令牌失效")).toBeNull();
    expect(screen.getByText("client@outlook.com")).toBeTruthy();
    expect(screen.getAllByRole("button", { name: "重新授权" })).toHaveLength(2);
    expect(accountsSpy).toHaveBeenNthCalledWith(
      1,
      { tenantID: "t1" },
      {
        refresh_status: "failed",
        page: 1,
        limit: 200,
      },
    );
    expect(accountsSpy).toHaveBeenNthCalledWith(
      2,
      { tenantID: "t1" },
      {
        refresh_status: "failed",
        page: 2,
        limit: 200,
      },
    );
  });
});
