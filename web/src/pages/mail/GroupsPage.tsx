import { Button } from "@cloudflare/kumo/components/button";
import { DropdownMenu } from "@cloudflare/kumo/components/dropdown";
import { ArrowDown, ArrowUp, DotsThree, PencilSimple, Plus, Trash } from "@phosphor-icons/react";
import { useCallback, useEffect, useState } from "react";
import { mailApi, type MailGroupNode } from "@/api/mail";
import { tenantApi, type QuotaUsage } from "@/api/tenant";
import { PageShell } from "@/components/layout/PageShell";
import { GroupDeleteDialog } from "@/components/mail/GroupDeleteDialog";
import { GroupDot } from "@/components/mail/GroupDot";
import { GroupFormDialog } from "@/components/mail/GroupFormDialog";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useTenantStore } from "@/store/tenantStore";

// 分组管理页：建分组、改名改色、排序、删除。分组是平的一层，
// 这一页就是那一层的全部内容。

type Dialog =
  | { kind: "create" }
  | { kind: "edit"; group: MailGroupNode }
  | { kind: "delete"; group: MailGroupNode };

export default function GroupsPage() {
  const tenantID = useTenantStore((s) => s.activeTenant?.id) ?? "";
  const [groups, setGroups] = useState<MailGroupNode[]>([]);
  const [quota, setQuota] = useState<QuotaUsage | null>(null);
  const [dialog, setDialog] = useState<Dialog | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const [reloadToken, setReloadToken] = useState(0);
  const reload = useCallback(() => setReloadToken((v) => v + 1), []);

  useEffect(() => {
    if (!tenantID) return undefined;
    let ignore = false;
    void (async () => {
      try {
        const [list, usage] = await Promise.all([
          mailApi.groups(tenantID),
          // 配额拿不到不该让整页报错：少一行「12 / 20」，
          // 远不如「分组明明取到了却显示加载失败」严重。
          tenantApi.quota(tenantID).catch(() => null),
        ]);
        if (ignore) return;
        setGroups(list.data);
        if (usage) setQuota(usage.data);
        setError("");
      } catch (e) {
        if (!ignore) setError(e instanceof Error ? e.message : "加载失败");
      } finally {
        if (!ignore) setLoading(false);
      }
    })();
    return () => {
      ignore = true;
    };
  }, [tenantID, reloadToken]);

  const total = groups.length;
  const limit = quota?.limits.max_groups ?? -1;
  const full = limit >= 0 && total >= limit;

  const saved = () => {
    setDialog(null);
    reload();
  };

  return (
    <PageShell
      title="分组"
      description={describeQuota(total, limit)}
      actions={
        <Button
          variant="primary"
          icon={Plus}
          disabled={full}
          // 满了就禁用而不是让它报 1001。配额是提前知道的，
          // 让用户填完一整个表单再被拒绝没有任何好处。
          title={full ? `已达套餐上限 ${limit} 个分组` : undefined}
          onClick={() => setDialog({ kind: "create" })}
        >
          新建分组
        </Button>
      }
    >
      {error && <p className="mb-4 text-sm text-kumo-danger">{error}</p>}

      {loading ? (
        <p className="text-sm text-kumo-subtle">加载中…</p>
      ) : (
        <ul className="max-w-3xl divide-y divide-kumo-hairline rounded-lg border border-kumo-line bg-kumo-elevated">
          {groups.map((group, index) => (
            <GroupRow
              key={group.id}
              group={group}
              index={index}
              groups={groups}
              tenantID={tenantID}
              onEdit={() => setDialog({ kind: "edit", group })}
              onDelete={() => setDialog({ kind: "delete", group })}
              onReordered={reload}
            />
          ))}
        </ul>
      )}

      <p className="mt-6 max-w-3xl text-xs text-kumo-subtle">
        删除分组时，它下面的账号会回到默认分组，不会被删除。
      </p>

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
    </PageShell>
  );
}

interface GroupRowProps {
  group: MailGroupNode;
  index: number;
  groups: MailGroupNode[];
  tenantID: string;
  onEdit: () => void;
  onDelete: () => void;
  onReordered: () => void;
}

function GroupRow({
  group,
  index,
  groups,
  tenantID,
  onEdit,
  onDelete,
  onReordered,
}: GroupRowProps) {
  const { pending, run } = useAsyncAction();

  // 重排接口收的是一批 ID：本地换好位置，整个顺序一起发。
  const move = (delta: number) =>
    void run(async () => {
      const next = [...groups];
      const [moved] = next.splice(index, 1);
      next.splice(index + delta, 0, moved);
      await mailApi.reorderGroups(
        tenantID,
        next.map((g) => g.id),
      );
      onReordered();
    });

  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <GroupDot color={group.color} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          <span className="truncate text-sm font-medium">{group.name}</span>
          {group.is_system && (
            <span className="shrink-0 rounded bg-kumo-tint px-1.5 py-0.5 text-[11px] text-kumo-subtle">
              默认
            </span>
          )}
        </div>
        {group.description && (
          <p className="mt-0.5 truncate text-xs text-kumo-subtle">{group.description}</p>
        )}
      </div>

      <span className="shrink-0 text-xs text-kumo-subtle tabular-nums">{group.account_count}</span>

      <DropdownMenu>
        <DropdownMenu.Trigger
          render={
            <Button
              size="sm"
              variant="ghost"
              icon={DotsThree}
              aria-label={`${group.name} 更多操作`}
              disabled={pending}
            />
          }
        />
        <DropdownMenu.Content>
          <DropdownMenu.Item icon={PencilSimple} onClick={onEdit}>
            编辑
          </DropdownMenu.Item>
          <DropdownMenu.Separator />
          <DropdownMenu.Item icon={ArrowUp} disabled={index <= 0} onClick={() => move(-1)}>
            上移
          </DropdownMenu.Item>
          <DropdownMenu.Item
            icon={ArrowDown}
            disabled={index >= groups.length - 1}
            onClick={() => move(1)}
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
            onClick={onDelete}
          >
            删除
          </DropdownMenu.Item>
        </DropdownMenu.Content>
      </DropdownMenu>
    </li>
  );
}

function describeQuota(total: number, limit: number): string {
  if (limit < 0) return `${total} 个分组，套餐不限数量`;
  return `${total} / ${limit} 个分组`;
}
