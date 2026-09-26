import { Button } from "@cloudflare/kumo/components/button";
import { Select } from "@cloudflare/kumo/components/select";
import { GearSix } from "@phosphor-icons/react";
import { useState } from "react";
import type { AccountStatus, MailGroupNode, TenantRef } from "@/api/mail";
import { GroupManagerDialog } from "./GroupManagerDialog";
import { groupSelectItems } from "./groupOptions";

const STATUS_ITEMS = [
  { label: "全部状态", value: "" },
  { label: "正常", value: "active" },
  { label: "已停用", value: "disabled" },
  { label: "已封禁", value: "banned" },
];

interface AccountFilterBarProps {
  tenantID: TenantRef;
  groups: MailGroupNode[];
  groupID: string | null;
  onGroupChange: (groupID: string | null) => void;
  status: AccountStatus | "";
  onStatusChange: (status: AccountStatus | "") => void;
  total: number;
  compact?: boolean;
  onGroupsChanged: () => void;
}

// 这一栏负责**过滤当前账号列表**；在左侧 MailSidebar 隐藏的断点，分组管理入口也
// 跟着分组筛选放在这里。导入/导出/刷新和批量动作仍在 MailToolbar 里——一个动作只
// 出现在一个地方，用户才不用猜「这两个刷新按钮是不是不一样」。

export function AccountFilterBar({
  tenantID,
  groups,
  groupID,
  onGroupChange,
  status,
  onStatusChange,
  total,
  compact = false,
  onGroupsChanged,
}: AccountFilterBarProps) {
  const [managerOpen, setManagerOpen] = useState(false);
  return (
    <header
      className={`flex shrink-0 flex-wrap items-center gap-3 border-b border-kumo-line px-4 py-3 ${
        compact ? "flex-col items-stretch gap-2 px-2 py-2" : ""
      }`}
    >
      {/* 分组下拉只在窄屏出现：≥1280 时左侧有完整的分组列表，这里再放一个是重复的。
          768~1280 收起左栏是 06 文档定的响应式方案，但分组切换不能跟着一起消失。 */}
      <div className={`flex min-w-0 items-center gap-2 xl:hidden ${compact ? "w-full" : ""}`}>
        <Select
          className={compact ? "min-w-0 flex-1" : "w-40"}
          size="sm"
          aria-label="按分组筛选"
          items={groupSelectItems(groups, { allLabel: "全部分组", counts: true })}
          value={groupID ?? ""}
          onValueChange={(value: string | null) => onGroupChange(value || null)}
        />
        <Button
          size="sm"
          variant="secondary"
          icon={GearSix}
          aria-label="管理分组"
          title="管理分组"
          onClick={() => setManagerOpen(true)}
        >
          {!compact && "管理"}
        </Button>
      </div>
      <Select
        className={compact ? "w-full" : "w-32"}
        size="sm"
        aria-label="按状态筛选"
        items={STATUS_ITEMS}
        value={status}
        onValueChange={(value: string | null) =>
          onStatusChange((value ?? "") as AccountStatus | "")
        }
      />
      <span className={compact ? "sr-only" : "text-sm text-kumo-subtle"}>共 {total} 个账号</span>

      {managerOpen && (
        <GroupManagerDialog
          tenantID={tenantID}
          groups={groups}
          onClose={() => setManagerOpen(false)}
          onGroupsChanged={onGroupsChanged}
        />
      )}
    </header>
  );
}
