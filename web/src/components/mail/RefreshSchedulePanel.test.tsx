import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { mailApi, type MailGroupNode } from "@/api/mail";
import { RefreshSchedulePanel } from "./RefreshSchedulePanel";

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
    refresh_interval_minutes: 0,
    next_refresh_at: null,
    proxy_url_masked: "",
    account_count: 3,
    ...extra,
  };
}

// Kumo 的 Select 把选项渲染在 portal 里，外面还套着几层 focus guard。
// 两个坑：① 触发器是 Base UI 的 combobox，靠 pointerdown 而不是 click 展开，
// 只发 click 的话列表根本不会出现；② 选项也要走完整的指针序列才会提交选择。
// 查选项用属性选择器而不是 getAllByRole，同 AGENTS.md §6.1 对 DropdownMenu 的说明。
async function pick(label: string) {
  const trigger = screen.getByLabelText("客户 A 的刷新间隔");
  await act(async () => {
    fireEvent.pointerDown(trigger);
    fireEvent.click(trigger);
  });
  const option = await waitFor(() => {
    const found = Array.from(document.querySelectorAll('[role="option"]')).find(
      (el) => el.textContent?.trim() === label,
    );
    if (!found) throw new Error(`没找到选项 ${label}`);
    return found;
  });
  await act(async () => {
    fireEvent.pointerDown(option);
    fireEvent.pointerUp(option);
    fireEvent.click(option);
  });
}

afterEach(() => {
  vi.restoreAllMocks();
});

describe("RefreshSchedulePanel", () => {
  // 关闭定时必须真的把 0 发出去。这个字段上 0 是有含义的取值，而后端的 PATCH
  // 是「不传即保持原值」——一旦哪次改动把它当成 falsy 过滤掉（`|| undefined`、
  // 展开时的条件成员之类），界面会显示已关闭，实际上还在按原周期刷。
  it("选择关闭时发送 refresh_interval_minutes: 0", async () => {
    const update = vi
      .spyOn(mailApi, "updateGroup")
      .mockResolvedValue({ code: 0, message: "", data: group() });
    const onSaved = vi.fn();

    await act(async () => {
      render(
        <RefreshSchedulePanel
          tenant="t1"
          groups={[group({ refresh_interval_minutes: 43200 })]}
          onSaved={onSaved}
        />,
      );
    });

    await pick("关闭");

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith("t1", "g1", { refresh_interval_minutes: 0 });
    });
    expect(onSaved).toHaveBeenCalled();
  });

  it("选择一个周期时发送对应的分钟数", async () => {
    const update = vi
      .spyOn(mailApi, "updateGroup")
      .mockResolvedValue({ code: 0, message: "", data: group() });

    await act(async () => {
      render(<RefreshSchedulePanel tenant="t1" groups={[group()]} onSaved={() => {}} />);
    });

    await pick("每 14 天");

    await waitFor(() => {
      expect(update).toHaveBeenCalledWith("t1", "g1", { refresh_interval_minutes: 20160 });
    });
  });

  // 保存失败要留在界面上。这类请求没有 toast 兜底（项目没有 toast 库），
  // 静默失败的表现是「我明明设过了，第二天发现没刷」。
  it("保存失败时把原因显示在该行上", async () => {
    vi.spyOn(mailApi, "updateGroup").mockRejectedValue(
      new Error("定时刷新间隔必须是 0（关闭）或 7~30 天"),
    );

    await act(async () => {
      render(<RefreshSchedulePanel tenant="t1" groups={[group()]} onSaved={() => {}} />);
    });

    await pick("每 14 天");

    await waitFor(() => {
      expect(screen.getByText(/定时刷新间隔必须是/)).toBeTruthy();
    });
  });
});
