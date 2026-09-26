import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Plus } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { asScope, mailApi, type MailGroupNode, type TenantRef } from "@/api/mail";
import { tenantApi } from "@/api/tenant";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { GroupDeleteDialog } from "./GroupDeleteDialog";
import { GroupFormDialog } from "./GroupFormDialog";
import { GroupList } from "./GroupList";

const UNLIMITED = -1;

type Dialog =
  | { kind: "create" }
  | { kind: "edit"; group: MailGroupNode }
  | { kind: "delete"; group: MailGroupNode };

interface GroupManagerDialogProps {
  tenantID: TenantRef;
  groups: MailGroupNode[];
  onClose: () => void;
  onGroupsChanged: () => void;
}

// 窄屏没有 MailSidebar，分组筛选旁的管理按钮用这个面板承接同一组操作。
// 桌面收起态则直接展开 MailSidebar，避免为同一套编辑能力维护两份入口。
export function GroupManagerDialog({
  tenantID,
  groups,
  onClose,
  onGroupsChanged,
}: GroupManagerDialogProps) {
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [menuContainer, setMenuContainer] = useState<HTMLDivElement | null>(null);
  const [maxGroups, setMaxGroups] = useState(UNLIMITED);
  const { pending, run } = useAsyncAction();

  const scope = asScope(tenantID);
  const scopeTenantID = scope.tenantID;
  const scopeAdmin = scope.admin ?? false;

  useEffect(() => {
    // 管理员查看别人的租户时不是成员，配额接口不对他开放；未知上限交给后端判定。
    if (scopeAdmin || !scopeTenantID) return undefined;
    let ignore = false;
    void tenantApi
      .quota(scopeTenantID)
      .then((r) => {
        if (!ignore) setMaxGroups(r.data.limits.max_groups);
      })
      .catch(() => {});
    return () => {
      ignore = true;
    };
  }, [scopeAdmin, scopeTenantID]);

  const full = maxGroups !== UNLIMITED && groups.length >= maxGroups;

  const move = (index: number, delta: number) =>
    void run(async () => {
      const next = [...groups];
      const [moved] = next.splice(index, 1);
      next.splice(index + delta, 0, moved);
      await mailApi.reorderGroups(
        tenantID,
        next.map((group) => group.id),
      );
      onGroupsChanged();
    });

  const saved = () => {
    setDialog(null);
    onGroupsChanged();
  };

  return (
    <div
      ref={setMenuContainer}
      data-group-manager-dialog
      className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4"
    >
      <LayerCard className="max-h-full w-full max-w-lg overflow-auto p-5">
        <div className="flex items-center justify-between gap-3">
          <h2 className="text-lg font-semibold text-kumo-strong">管理分组</h2>
          <Button
            size="sm"
            variant="secondary"
            icon={Plus}
            disabled={full}
            title={full ? `已达套餐上限 ${maxGroups} 个分组` : "新建分组"}
            onClick={() => setDialog({ kind: "create" })}
          >
            新建
          </Button>
        </div>

        <div className="mt-4">
          <GroupList
            groups={groups}
            selectedID={undefined}
            onSelect={(groupID) => {
              const group = groups.find((item) => item.id === groupID);
              if (group) setDialog({ kind: "edit", group });
            }}
            showAll={false}
            actionsAlwaysVisible
            menuContainer={menuContainer}
            actions={{
              onEdit: (group) => setDialog({ kind: "edit", group }),
              onDelete: (group) => setDialog({ kind: "delete", group }),
              onMove: move,
              pending,
            }}
          />
        </div>

        <div className="mt-5 flex justify-end">
          <Button type="button" variant="secondary" onClick={onClose}>
            关闭
          </Button>
        </div>
      </LayerCard>

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
    </div>
  );
}
