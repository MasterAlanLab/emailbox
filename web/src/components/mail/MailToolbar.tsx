import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import {
  ArrowsClockwise,
  Download,
  Prohibit,
  Trash,
  Upload,
  CheckCircle,
} from "@phosphor-icons/react";
import { useMemo } from "react";
import { mailApi, type TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useSelectionStore } from "@/store/selectionStore";

// 全局动作条。把原先散在筛选栏（导入/导出/刷新）和 AccountBatchMenu（停用/启用/删除）
// 里的动作收到同一行——那两处一个在列表上方、一个只在有选中时才出现，
// 用户得先知道「选中之后会多出一条」才找得到批量删除。
//
// 这里**只放我们真的做得到的动作**。参照设计里还有「邮件规则 / 新建邮件 / 群发邮件」，
// 那三个后端没有对应能力，摆上去就是三个点了没反应的按钮。
//
// 批量动作常驻但在无选中时禁用，不跟着选中状态出现/消失：
// 让工具条高度恒定，列表不会因为勾了一个复选框就整体往下跳一行。

interface MailToolbarProps {
  tenantID: TenantRef;
  onReload: () => void;
  onExport: () => void;
}

export function MailToolbar({ tenantID, onReload, onExport }: MailToolbarProps) {
  // 稳定引用：取回 Set 再 useMemo 成数组。直接在选择器里 Array.from
  // 会让 zustand v5 的快照每次都不同，进而无限重渲染（AGENTS.md §6.3）。
  const selected = useSelectionStore((s) => s.selected);
  const ids = useMemo(() => Array.from(selected), [selected]);
  const clear = useSelectionStore((s) => s.clear);
  const { error, pending, run } = useAsyncAction();

  const none = ids.length === 0;
  const act = (fn: () => Promise<unknown>) =>
    void run(async () => {
      await fn();
      clear();
      onReload();
    });

  return (
    <div className="flex h-(--ebx-toolbar-h) shrink-0 items-center gap-2 overflow-x-auto border-b border-kumo-line px-4">
      <LinkButton href="/mail/import" variant="primary" icon={Upload}>
        导入邮箱
      </LinkButton>
      <Button variant="secondary" icon={Download} onClick={onExport}>
        导出
      </Button>
      <Button variant="secondary" icon={ArrowsClockwise} onClick={onReload}>
        刷新
      </Button>

      <span className="mx-1 h-5 w-px shrink-0 bg-kumo-line" aria-hidden />

      <Button
        variant="secondary"
        icon={CheckCircle}
        disabled={none || pending}
        onClick={() => act(() => mailApi.batchStatus(tenantID, ids, "active"))}
      >
        启用
      </Button>
      <Button
        variant="secondary"
        icon={Prohibit}
        disabled={none || pending}
        onClick={() => act(() => mailApi.batchStatus(tenantID, ids, "disabled"))}
      >
        停用
      </Button>
      <Button
        variant="secondary-destructive"
        icon={Trash}
        disabled={none || pending}
        onClick={() => act(() => mailApi.batchDelete(tenantID, ids))}
      >
        删除
      </Button>

      {!none && (
        <span className="ml-2 flex shrink-0 items-center gap-2 text-sm text-kumo-subtle">
          已选中 {ids.length} 个
          <button type="button" className="text-kumo-link hover:underline" onClick={clear}>
            取消
          </button>
        </span>
      )}
      {error && <span className="ml-2 shrink-0 text-sm text-kumo-danger">{error}</span>}
    </div>
  );
}
