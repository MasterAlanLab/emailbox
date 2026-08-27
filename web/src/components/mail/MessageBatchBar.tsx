import { Button } from "@cloudflare/kumo/components/button";
import { EnvelopeOpen, Trash } from "@phosphor-icons/react";
import { mailApi, type Message, type MessageRef, type TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { toRef } from "./messageRef";

interface MessageBatchBarProps {
  tenantID: TenantRef;
  accountID: string;
  messages: Message[];
  onRead: (done: MessageRef[]) => void;
  onDelete: (done: MessageRef[]) => void;
}

// MessageBatchBar 只在有选中项时出现，行为与账号列表那条保持一致。
export function MessageBatchBar({
  tenantID,
  accountID,
  messages,
  onRead,
  onDelete,
}: MessageBatchBarProps) {
  const { error, pending, run } = useAsyncAction();
  if (messages.length === 0) return null;

  const refs = messages.map(toRef);

  // 后端逐封返回成功与否（部分失败是常态：一封信可能已经在别处被删了），
  // 只把 ok 的那些应用到界面上，失败的留在原地并把条数说清楚。
  const act = (
    call: () => Promise<{ items: { ref: MessageRef; ok: boolean }[]; failed: number }>,
    apply: (done: MessageRef[]) => void,
  ) =>
    void run(async () => {
      const result = await call();
      apply(result.items.filter((i) => i.ok).map((i) => i.ref));
      if (result.failed > 0) throw new Error(`有 ${result.failed} 封操作失败`);
    });

  return (
    <div className="flex flex-wrap items-center gap-2 border-b border-kumo-line bg-kumo-tint px-3 py-2 text-sm">
      <span>已选中 {messages.length} 封</span>
      <Button
        size="sm"
        variant="secondary"
        icon={EnvelopeOpen}
        disabled={pending}
        onClick={() =>
          act(() => mailApi.markMessagesRead(tenantID, accountID, refs).then((r) => r.data), onRead)
        }
      >
        标记已读
      </Button>
      <Button
        size="sm"
        variant="secondary-destructive"
        icon={Trash}
        disabled={pending}
        onClick={() =>
          act(() => mailApi.deleteMessages(tenantID, accountID, refs).then((r) => r.data), onDelete)
        }
      >
        删除
      </Button>
      {error && <span className="text-kumo-danger">{error}</span>}
    </div>
  );
}
