import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowDown, ArrowUp, DotsThree, Folder, PencilSimple, Trash } from "@phosphor-icons/react";
import type { MailGroupNode } from "@/api/mail";
import { SidebarRow } from "./SidebarRow";

// 左栏的分组列表。分组是平的一层，所以这里没有展开/折叠，也没有缩进——
// 一行就是一个分组，点它就是按它筛选。
//
// 行的外观复用左栏的 SidebarRow：分组行和上面的状态行长得一模一样，
// 用户才会把两段读成「同一层级的两组筛选」，而不是「一个是导航一个是筛选」。
//
// 管理动作（改名改色、排序、删除）挂在每行悬停才显形的 ⋯ 菜单里：
// 它们是低频动作，常显会把「筛选」这个高频用途淹掉。

export interface GroupActions {
  onEdit: (group: MailGroupNode) => void;
  onDelete: (group: MailGroupNode) => void;
  /** delta 为 -1 上移、+1 下移；index 是该分组在当前顺序里的位置。 */
  onMove: (index: number, delta: number) => void;
  /** 重排请求在飞时禁用菜单，避免连点发出两次相互矛盾的顺序。 */
  pending: boolean;
}

interface GroupListProps {
  groups: MailGroupNode[];
  selectedID: string | null;
  onSelect: (groupID: string | null) => void;
  className?: string;
  /** 不传就是纯筛选列表（管理员看别人租户时用不到这些动作）。 */
  actions?: GroupActions;
}

export function GroupList({ groups, selectedID, onSelect, className, actions }: GroupListProps) {
  return (
    <nav className={className} aria-label="邮箱分组">
      <SidebarRow
        label="全部账号"
        count={groups.reduce((sum, g) => sum + g.account_count, 0)}
        selected={selectedID === null}
        onSelect={() => onSelect(null)}
        leading={<Folder size={14} className="shrink-0 text-kumo-subtle" />}
      />
      {groups.map((group, index) => (
        <SidebarRow
          key={group.id}
          label={group.name}
          count={group.account_count}
          selected={selectedID === group.id}
          onSelect={() => onSelect(group.id)}
          leading={<Folder size={14} className="shrink-0 text-kumo-subtle" />}
          trailing={
            actions && (
              <GroupRowMenu group={group} index={index} total={groups.length} actions={actions} />
            )
          }
        />
      ))}
    </nav>
  );
}

function GroupRowMenu({
  group,
  index,
  total,
  actions,
}: {
  group: MailGroupNode;
  index: number;
  total: number;
  actions: GroupActions;
}) {
  return (
    <DropdownMenu>
      <DropdownMenu.Trigger
        render={
          <Button
            size="sm"
            variant="ghost"
            icon={DotsThree}
            aria-label={`${group.name} 更多操作`}
            disabled={actions.pending}
          />
        }
      />
      <DropdownMenu.Content>
        <DropdownMenu.Item icon={PencilSimple} onClick={() => actions.onEdit(group)}>
          编辑
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        <DropdownMenu.Item
          icon={ArrowUp}
          disabled={index <= 0}
          onClick={() => actions.onMove(index, -1)}
        >
          上移
        </DropdownMenu.Item>
        <DropdownMenu.Item
          icon={ArrowDown}
          disabled={index >= total - 1}
          onClick={() => actions.onMove(index, 1)}
        >
          下移
        </DropdownMenu.Item>
        <DropdownMenu.Separator />
        {/* 系统分组删不掉——后端 GroupService.Delete 拦 is_system，
            而且它是所有账号的回落目标，没了之后删任何分组都会失败。 */}
        <DropdownMenu.Item
          icon={Trash}
          variant="danger"
          disabled={group.is_system}
          onClick={() => actions.onDelete(group)}
        >
          删除
        </DropdownMenu.Item>
      </DropdownMenu.Content>
    </DropdownMenu>
  );
}
