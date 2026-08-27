import { ArrowsClockwise, CaretDown, CaretUp } from "@phosphor-icons/react";
import { useState } from "react";
import { Link } from "react-router-dom";
import type { MailGroupNode, RefreshStatus } from "@/api/mail";
import type { RefreshStats } from "@/api/jobs";
import { GroupList } from "./GroupList";
import { SidebarRow } from "./SidebarRow";
import { StatusDot, type StatusTone } from "./StatusDot";

// 左栏：CTA + 账号状态 + 分组列表。
//
// 状态和分组是**两个并列的维度**，不是嵌套的：状态段筛的是 refresh_status
// （登录成功/失败/从未），分组段筛的是用户自己建的分组。
// 把状态挂到每个分组底下（参照设计的做法）在我们这里行不通——
// 它只有三个一级项，而用户的分组数量不定，每个都展开四个子项会让面板长到没法用。
// 两段并列还有个好处：两个维度可以叠加（「客户 A 里登录失败的账号」）。

const STATUS_ITEMS: { value: RefreshStatus | ""; label: string; tone: StatusTone }[] = [
  { value: "", label: "全部", tone: "unread" },
  { value: "success", label: "登录成功", tone: "online" },
  { value: "failed", label: "登录失败", tone: "error" },
  { value: "never", label: "尚未登录", tone: "idle" },
];

interface MailSidebarProps {
  groups: MailGroupNode[];
  groupID: string | null;
  onGroupChange: (groupID: string | null) => void;
  refreshStatus: RefreshStatus | "";
  onRefreshStatusChange: (status: RefreshStatus | "") => void;
  // stats 由 MailPage 取一次分给左栏和状态栏共用，两处不各拉一遍。
  stats: RefreshStats | null;
}

export function MailSidebar({
  groups,
  groupID,
  onGroupChange,
  refreshStatus,
  onRefreshStatusChange,
  stats,
}: MailSidebarProps) {
  const [groupsOpen, setGroupsOpen] = useState(true);

  const countOf = (value: RefreshStatus | "") => {
    if (!stats) return undefined;
    if (value === "") return stats.total;
    return stats[value];
  };

  return (
    // 背景用 base 而不是 canvas：左边紧挨着的全局导航栏已经是 canvas，
    // 两块同色面板贴在一起会糊成一片，看不出哪里是「导航」哪里是「这个页面的内容」。
    <aside className="hidden w-(--ebx-sidebar-w) shrink-0 flex-col overflow-y-auto border-r border-kumo-line bg-kumo-base xl:flex">
      {/* 主操作提到最显眼的位置。参照设计这里是「开始收件」，我们没有一键收件，
          最接近的高频入口是批量刷新令牌——账号能不能用，取决于令牌新不新。
          按钮写它真正会做的事，不借用一个我们做不到的名字。 */}
      <div className="p-3">
        <Link
          to="/mail/tokens"
          className="flex items-center justify-center gap-2 rounded-2xl bg-linear-to-br from-ebx-brand-grad-from to-ebx-brand-grad-to px-4 py-3 text-sm font-medium text-white shadow-sm transition hover:opacity-90"
        >
          <ArrowsClockwise size={18} weight="bold" />
          批量刷新令牌
        </Link>
      </div>

      <Section title="账号状态">
        {STATUS_ITEMS.map((item) => (
          <SidebarRow
            key={item.value || "all"}
            label={item.label}
            count={countOf(item.value)}
            selected={refreshStatus === item.value}
            onSelect={() => onRefreshStatusChange(item.value)}
            leading={<StatusDot tone={item.tone} />}
          />
        ))}
      </Section>

      <Section
        title="邮箱分组"
        action={
          <button
            type="button"
            className="grid size-5 place-items-center rounded text-kumo-subtle hover:text-kumo-strong"
            onClick={() => setGroupsOpen((v) => !v)}
            aria-label={groupsOpen ? "折叠分组" : "展开分组"}
            aria-expanded={groupsOpen}
          >
            {groupsOpen ? <CaretUp size={12} /> : <CaretDown size={12} />}
          </button>
        }
      >
        {groupsOpen && <GroupList groups={groups} selectedID={groupID} onSelect={onGroupChange} />}
      </Section>
    </aside>
  );
}

function Section({
  title,
  action,
  children,
}: {
  title: string;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="px-2 pb-2">
      <div className="flex items-center justify-between px-2 py-1.5">
        <h2 className="text-xs font-medium tracking-wide text-kumo-subtle uppercase">{title}</h2>
        {action}
      </div>
      {children}
    </div>
  );
}
