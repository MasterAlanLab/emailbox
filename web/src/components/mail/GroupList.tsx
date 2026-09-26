import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowDown, ArrowUp, DotsThree, Folder, PencilSimple, Trash } from "@phosphor-icons/react";
import type { MailGroupNode } from "@/api/mail";
import { SidebarRow } from "./SidebarRow";

// 分组是平的一层，所以这里没有展开/折叠，也没有缩进——
// 侧栏里点行用于筛选；窄屏管理面板复用行样式，点行直接编辑。
//
// 行的外观复用左栏的 SidebarRow：分组行和上面的状态行长得一模一样，
// 用户才会把两段读成「同一层级的两组筛选」，而不是「一个是导航一个是筛选」。
//
// 侧栏里的管理动作挂在悬停才显形的 ⋯ 菜单里，避免淹没高频筛选；
// 管理面板在触屏上使用，菜单持续可见。

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
  /** undefined 表示只用于管理面板，不突出任何筛选项。 */
  selectedID?: string | null;
  onSelect: (groupID: string | null) => void;
  className?: string;
  /** 管理面板只列出实际分组，不显示用于筛选的「全部账号」行。 */
  showAll?: boolean;
  /** 管理面板在触屏上没有 hover，操作菜单要一直显示。 */
  actionsAlwaysVisible?: boolean;
  /** 弹层宿主。嵌套在管理面板时必须留在同一个 stacking context 内。 */
  menuContainer?: HTMLElement | null;
  /** 不传就是纯筛选列表（管理员看别人租户时用不到这些动作）。 */
  actions?: GroupActions;
}

export function GroupList({
  groups,
  selectedID,
  onSelect,
  className,
  showAll = true,
  actionsAlwaysVisible = false,
  menuContainer,
  actions,
}: GroupListProps) {
  return (
    <nav className={className} aria-label="邮箱分组">
      {showAll && (
        <SidebarRow
          label="全部账号"
          count={groups.reduce((sum, g) => sum + g.account_count, 0)}
          selected={selectedID === null}
          onSelect={() => onSelect(null)}
          leading={<Folder size={14} className="shrink-0 text-kumo-subtle" />}
        />
      )}
      {groups.map((group, index) => (
        <SidebarRow
          key={group.id}
          label={group.name}
          count={group.account_count}
          selected={selectedID === group.id}
          onSelect={() => onSelect(group.id)}
          leading={<Folder size={14} className="shrink-0 text-kumo-subtle" />}
          alwaysShowTrailing={actionsAlwaysVisible}
          trailing={
            actions && (
              <GroupRowMenu
                group={group}
                index={index}
                total={groups.length}
                actions={actions}
                menuContainer={menuContainer}
              />
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
  menuContainer,
}: {
  group: MailGroupNode;
  index: number;
  total: number;
  actions: GroupActions;
  menuContainer?: HTMLElement | null;
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
      {/* Kumo 的 className 会落在内层 MenuPopup，不能改变 portal 外层
          MenuPositioner 的层级。这里同时给 positioner 传 z-index，否则菜单
          虽然已经挂进管理面板，仍会被面板自己的 LayerCard 盖住。 */}
      <DropdownMenu.Content className="z-[60]" style={{ zIndex: 60 }} container={menuContainer}>
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
