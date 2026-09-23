import { Button } from "@cloudflare/kumo/components/button";
import { CaretLeft, CaretRight, Plus } from "@phosphor-icons/react";
import { useCallback, useEffect, useState } from "react";
import {
  asScope,
  mailApi,
  type MailGroupNode,
  type RefreshStatus,
  type TenantRef,
} from "@/api/mail";
import type { RefreshStats } from "@/api/jobs";
import { tenantApi } from "@/api/tenant";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { CompactMailFilters } from "./CompactMailFilters";
import { GroupDeleteDialog } from "./GroupDeleteDialog";
import { GroupFormDialog } from "./GroupFormDialog";
import { GroupList } from "./GroupList";
import { SidebarRow } from "./SidebarRow";
import { StatusDot, type StatusTone } from "./StatusDot";

// 左栏：账号状态 + 分组列表。
//
// 状态和分组是**两个并列的维度**，不是嵌套的：状态段筛的是 refresh_status
// （登录成功/失败/从未），分组段筛的是用户自己建的分组。
// 把状态挂到每个分组底下（参照设计的做法）在我们这里行不通——
// 它只有三个一级项，而用户的分组数量不定，每个都展开四个子项会让面板长到没法用。
// 两段并列还有个好处：两个维度可以叠加（「客户 A 里登录失败的账号」）。
//
// 分组的增删改也在这一段里。它原本是一个一级菜单 /mail/groups，
// 但那一页从头到尾只有「分组管理」一件事，撑不起一个和「邮箱」并列的入口；
// 而且管理分组的人几乎总是刚在这里看完某个分组有多少账号。
// 就近放在列表上，比让用户跳出工作台再跳回来短得多。

const STATUS_ITEMS: { value: RefreshStatus | ""; label: string; tone: StatusTone }[] = [
  { value: "", label: "全部账号", tone: "unread" },
  { value: "success", label: "凭据正常", tone: "online" },
  { value: "failed", label: "授权失效", tone: "error" },
  { value: "never", label: "未检测", tone: "idle" },
];

// 与后端 model.Unlimited 对应：-1 表示不限。上限未知时也用它——
// 未知不该表现成「已满」，否则配额接口一挂，新建按钮就全灰了。
const UNLIMITED = -1;

type Dialog =
  | { kind: "create" }
  | { kind: "edit"; group: MailGroupNode }
  | { kind: "delete"; group: MailGroupNode };

interface MailSidebarProps {
  tenantID: TenantRef;
  /** 当前正在查看的账号。选中后侧栏自动缩成图标列，给邮件列表让出空间。 */
  activeAccountID?: string | null;
  groups: MailGroupNode[];
  groupID: string | null;
  onGroupChange: (groupID: string | null) => void;
  refreshStatus: RefreshStatus | "";
  onRefreshStatusChange: (status: RefreshStatus | "") => void;
  // stats 由 MailPage 取一次分给左栏和状态栏共用，两处不各拉一遍。
  stats: RefreshStats | null;
  /** 分组增删改排序之后让 MailPage 重新取数——账号列表的分组名会跟着变。 */
  onGroupsChanged: () => void;
}

export function MailSidebar({
  tenantID,
  activeAccountID = null,
  groups,
  groupID,
  onGroupChange,
  refreshStatus,
  onRefreshStatusChange,
  stats,
  onGroupsChanged,
}: MailSidebarProps) {
  const [storedCollapsed, toggleStoredCollapsed] = usePersistedOpen(
    "emailbox.mail.sidebar.collapsed",
    false,
  );
  // 自动折叠不覆盖用户的长期偏好：没有选中账号时回到 localStorage 里的状态；
  // 正在看信时，用户仍可点一次箭头临时展开侧栏。用渲染期同步重置，避免 effect
  // 晚一帧把上一个账号的「已展开」状态带到新账号上。
  const [focusState, setFocusState] = useState<{
    accountID: string | null;
    manuallyExpanded: boolean;
  }>(() => ({ accountID: activeAccountID, manuallyExpanded: false }));
  let focusView = focusState;
  if (focusState.accountID !== activeAccountID) {
    focusView = { accountID: activeAccountID, manuallyExpanded: false };
    setFocusState(focusView);
  }
  const collapsed = activeAccountID ? !focusView.manuallyExpanded : storedCollapsed;
  const toggleCollapsed = () => {
    if (activeAccountID) {
      setFocusState((prev) => ({ ...prev, manuallyExpanded: !prev.manuallyExpanded }));
    } else {
      toggleStoredCollapsed();
    }
  };
  const [statusOpen, toggleStatus] = usePersistedOpen("emailbox.mail.sidebar.status");
  const [groupsOpen, toggleGroups] = usePersistedOpen("emailbox.mail.sidebar.groups");
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [maxGroups, setMaxGroups] = useState(UNLIMITED);
  const { pending, run } = useAsyncAction();

  const scope = asScope(tenantID);
  const scopeTenantID = scope.tenantID;
  const scopeAdmin = scope.admin ?? false;

  useEffect(() => {
    // 管理员看别人的租户时拿不到配额：/tenants/:id/quota 是成员接口，
    // 他不是那个租户的成员。上限未知就不提前禁用，真超了后端还会拦。
    if (scopeAdmin || !scopeTenantID) return undefined;
    let ignore = false;
    void tenantApi
      .quota(scopeTenantID)
      .then((r) => {
        if (!ignore) setMaxGroups(r.data.limits.max_groups);
      })
      // 配额拿不到只是少一层提前禁用，不该让分组列表跟着报错。
      .catch(() => {});
    return () => {
      ignore = true;
    };
  }, [scopeAdmin, scopeTenantID]);

  const countOf = (value: RefreshStatus | "") => {
    if (!stats) return undefined;
    if (value === "") return stats.total;
    return stats[value];
  };

  // 满了就禁用而不是让用户填完一整个表单再撞后端的 1001。
  const full = maxGroups !== UNLIMITED && groups.length >= maxGroups;

  // 重排接口收的是一批 ID：本地换好位置，整个顺序一起发。
  const move = (index: number, delta: number) =>
    void run(async () => {
      const next = [...groups];
      const [moved] = next.splice(index, 1);
      next.splice(index + delta, 0, moved);
      await mailApi.reorderGroups(
        tenantID,
        next.map((g) => g.id),
      );
      onGroupsChanged();
    });

  const saved = () => {
    setDialog(null);
    onGroupsChanged();
  };

  return (
    // 背景用 base 而不是 canvas：左边紧挨着的全局导航栏已经是 canvas，
    // 两块同色面板贴在一起会糊成一片，看不出哪里是「导航」哪里是「这个页面的内容」。
    <aside
      className={`hidden shrink-0 flex-col overflow-y-auto border-r border-kumo-line bg-kumo-base transition-[width] duration-200 xl:flex ${
        collapsed ? "w-14" : "w-(--ebx-sidebar-w)"
      }`}
    >
      <div className="flex h-11 shrink-0 items-center justify-end border-b border-kumo-line px-2">
        <button
          type="button"
          aria-label={collapsed ? "展开邮箱侧栏" : "收起邮箱侧栏"}
          aria-expanded={!collapsed}
          title={collapsed ? "展开邮箱侧栏" : "收起邮箱侧栏"}
          onClick={toggleCollapsed}
          className="grid size-8 place-items-center rounded-lg text-kumo-subtle hover:bg-kumo-interact hover:text-kumo-strong"
        >
          {collapsed ? <CaretRight size={16} /> : <CaretLeft size={16} />}
        </button>
      </div>

      {collapsed ? (
        <CompactMailFilters
          items={STATUS_ITEMS}
          groups={groups}
          groupID={groupID}
          onGroupChange={onGroupChange}
          refreshStatus={refreshStatus}
          onRefreshStatusChange={onRefreshStatusChange}
          stats={stats}
        />
      ) : (
        <>
          <Section title="账号状态" open={statusOpen} onToggle={toggleStatus}>
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
            open={groupsOpen}
            onToggle={toggleGroups}
            action={
              <Button
                size="sm"
                variant="ghost"
                icon={Plus}
                aria-label="新建分组"
                disabled={full}
                title={full ? `已达套餐上限 ${maxGroups} 个分组` : "新建分组"}
                onClick={() => setDialog({ kind: "create" })}
              />
            }
          >
            <GroupList
              groups={groups}
              selectedID={groupID}
              onSelect={onGroupChange}
              actions={{
                onEdit: (group) => setDialog({ kind: "edit", group }),
                onDelete: (group) => setDialog({ kind: "delete", group }),
                onMove: move,
                pending,
              }}
            />
          </Section>
        </>
      )}

      {dialog?.kind === "create" && (
        <GroupFormDialog tenantID={tenantID} onClose={() => setDialog(null)} onSaved={saved} />
      )}
      {dialog?.kind === "edit" && (
        <GroupFormDialog
          tenantID={tenantID}
          group={dialog.group}
          onClose={() => setDialog(null)}
          onSaved={saved}
        />
      )}
      {dialog?.kind === "delete" && (
        <GroupDeleteDialog
          tenantID={tenantID}
          group={dialog.group}
          onClose={() => setDialog(null)}
          onDeleted={saved}
        />
      )}
    </aside>
  );
}

function Section({
  title,
  open,
  onToggle,
  action,
  children,
}: {
  title: string;
  open: boolean;
  onToggle: () => void;
  action?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="px-2 pb-2">
      <div className="flex items-center gap-1 px-2 py-1.5">
        {/* 整个标题都是折叠开关，不是只有那个小三角——12px 的三角作为
            唯一命中区太难点了。 */}
        <button
          type="button"
          onClick={onToggle}
          aria-expanded={open}
          className="flex min-w-0 flex-1 items-center gap-1 text-left text-xs font-medium tracking-wide text-kumo-subtle uppercase hover:text-kumo-strong"
        >
          <CaretRight
            size={10}
            weight="bold"
            className={`shrink-0 transition-transform ${open ? "rotate-90" : ""}`}
          />
          <span className="truncate">{title}</span>
        </button>
        {action}
      </div>
      {open && children}
    </div>
  );
}

// 折叠状态记在 localStorage，理由同 AppSidebar 的收起：它是很强的个人偏好。
// 只用一两个分组的人会把分组段一直收着；而窄版侧栏默认展开，首次进入仍能看到完整筛选。
// defaultOpen 让同一个小钩子也能服务于侧栏整体（默认收起）和两个内容段（默认展开）。
function usePersistedOpen(key: string, defaultOpen = true): [boolean, () => void] {
  const [open, setOpen] = useState(() => {
    try {
      const saved = localStorage.getItem(key);
      return saved === null ? defaultOpen : saved !== "false";
    } catch {
      return defaultOpen;
    }
  });

  const toggle = useCallback(() => {
    setOpen((prev) => {
      const next = !prev;
      try {
        localStorage.setItem(key, String(next));
      } catch {
        // 隐私模式下写不了。记不住偏好无所谓，不能因此让按钮点了没反应。
      }
      return next;
    });
  }, [key]);

  return [open, toggle];
}
