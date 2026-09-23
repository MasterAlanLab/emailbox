import { Folder } from "@phosphor-icons/react";
import type { RefreshStats } from "@/api/jobs";
import type { MailGroupNode, RefreshStatus } from "@/api/mail";
import { GroupDot } from "./GroupDot";
import { StatusDot, type StatusTone } from "./StatusDot";

export interface CompactFilterItem {
  value: RefreshStatus | "";
  label: string;
  tone: StatusTone;
}

interface CompactMailFiltersProps {
  items: CompactFilterItem[];
  groups: MailGroupNode[];
  groupID: string | null;
  onGroupChange: (groupID: string | null) => void;
  refreshStatus: RefreshStatus | "";
  onRefreshStatusChange: (status: RefreshStatus | "") => void;
  stats: RefreshStats | null;
}

// 工作台侧栏收起后的窄版筛选。文字和低频管理动作收起来，筛选仍保留为图标，
// 这样用户不必为了切换分组重新展开侧栏。
export function CompactMailFilters({
  items,
  groups,
  groupID,
  onGroupChange,
  refreshStatus,
  onRefreshStatusChange,
  stats,
}: CompactMailFiltersProps) {
  return (
    <div className="flex flex-col items-center gap-1 px-2 py-2">
      <p className="sr-only">邮箱筛选</p>
      {items.map((item) => (
        <button
          key={item.value || "all"}
          type="button"
          aria-label={`${item.label}${stats ? ` ${item.value ? stats[item.value] : stats.total}` : ""}`}
          aria-current={refreshStatus === item.value ? "true" : undefined}
          title={`${item.label}${stats ? `（${item.value ? stats[item.value] : stats.total}）` : ""}`}
          onClick={() => onRefreshStatusChange(item.value)}
          className={`grid size-9 place-items-center rounded-lg ${
            refreshStatus === item.value
              ? "bg-kumo-tint text-kumo-strong"
              : "text-kumo-subtle hover:bg-kumo-interact hover:text-kumo-strong"
          }`}
        >
          <StatusDot tone={item.tone} />
        </button>
      ))}
      <div className="my-1 h-px w-8 bg-kumo-line" />
      <button
        type="button"
        aria-label="全部账号"
        aria-current={groupID === null ? "true" : undefined}
        title="全部账号"
        onClick={() => onGroupChange(null)}
        className={`grid size-9 place-items-center rounded-lg ${
          groupID === null
            ? "bg-kumo-tint text-kumo-strong"
            : "text-kumo-subtle hover:bg-kumo-interact hover:text-kumo-strong"
        }`}
      >
        <Folder size={15} />
      </button>
      {groups.map((group) => (
        <button
          key={group.id}
          type="button"
          aria-label={group.name}
          aria-current={groupID === group.id ? "true" : undefined}
          title={`${group.name}（${group.account_count}）`}
          onClick={() => onGroupChange(group.id)}
          className={`grid size-9 place-items-center rounded-lg ${
            groupID === group.id
              ? "bg-kumo-tint text-kumo-strong"
              : "text-kumo-subtle hover:bg-kumo-interact hover:text-kumo-strong"
          }`}
        >
          <GroupDot color={group.color} />
        </button>
      ))}
    </div>
  );
}
