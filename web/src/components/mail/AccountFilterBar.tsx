import { Select } from "@cloudflare/kumo/components/select";
import type { AccountStatus, MailGroupNode } from "@/api/mail";
import { groupSelectItems } from "./groupOptions";

const STATUS_ITEMS = [
  { label: "全部状态", value: "" },
  { label: "正常", value: "active" },
  { label: "已停用", value: "disabled" },
  { label: "已封禁", value: "banned" },
];

interface AccountFilterBarProps {
  groups: MailGroupNode[];
  groupID: string | null;
  onGroupChange: (groupID: string | null) => void;
  status: AccountStatus | "";
  onStatusChange: (status: AccountStatus | "") => void;
  total: number;
}

// 这一栏只负责**过滤当前账号列表**。导入/导出/刷新和批量动作都在 MailToolbar 里——
// 一个动作只出现在一个地方，用户才不用猜「这两个刷新按钮是不是不一样」。

export function AccountFilterBar({
  groups,
  groupID,
  onGroupChange,
  status,
  onStatusChange,
  total,
}: AccountFilterBarProps) {
  return (
    <header className="flex shrink-0 flex-wrap items-center gap-3 border-b border-kumo-line px-4 py-3">
      {/* 分组下拉只在窄屏出现：≥1280 时左侧有完整的分组列表，这里再放一个是重复的。
          768~1280 收起左栏是 06 文档定的响应式方案，但分组切换不能跟着一起消失。 */}
      <Select
        className="w-40 xl:hidden"
        size="sm"
        aria-label="按分组筛选"
        items={groupSelectItems(groups, { allLabel: "全部分组", counts: true })}
        value={groupID ?? ""}
        onValueChange={(value: string | null) => onGroupChange(value || null)}
      />
      <Select
        className="w-32"
        size="sm"
        aria-label="按状态筛选"
        items={STATUS_ITEMS}
        value={status}
        onValueChange={(value: string | null) =>
          onStatusChange((value ?? "") as AccountStatus | "")
        }
      />
      <span className="text-sm text-kumo-subtle">共 {total} 个账号</span>
    </header>
  );
}
