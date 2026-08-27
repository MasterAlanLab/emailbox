import { Folder } from "@phosphor-icons/react";
import type { MailGroupNode } from "@/api/mail";
import { SidebarRow } from "./SidebarRow";

// 左栏的分组列表。分组是平的一层，所以这里没有展开/折叠，也没有缩进——
// 一行就是一个分组，点它就是按它筛选。
//
// 行的外观复用左栏的 SidebarRow：分组行和上面的状态行长得一模一样，
// 用户才会把两段读成「同一层级的两组筛选」，而不是「一个是导航一个是筛选」。

interface GroupListProps {
  groups: MailGroupNode[];
  selectedID: string | null;
  onSelect: (groupID: string | null) => void;
  className?: string;
}

export function GroupList({ groups, selectedID, onSelect, className }: GroupListProps) {
  return (
    <nav className={className} aria-label="邮箱分组">
      <SidebarRow
        label="全部账号"
        count={groups.reduce((sum, g) => sum + g.account_count, 0)}
        selected={selectedID === null}
        onSelect={() => onSelect(null)}
        leading={<Folder size={14} className="shrink-0 text-kumo-subtle" />}
      />
      {groups.map((group) => (
        <SidebarRow
          key={group.id}
          label={group.name}
          count={group.account_count}
          selected={selectedID === group.id}
          onSelect={() => onSelect(group.id)}
          leading={<Folder size={14} className="shrink-0 text-kumo-subtle" />}
        />
      ))}
    </nav>
  );
}
