import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { mailApi, type MailGroupNode, type TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";

interface GroupDeleteDialogProps {
  tenantID: TenantRef;
  group: MailGroupNode;
  onClose: () => void;
  onDeleted: () => void;
}

// 删除分组的确认框。它存在的唯一理由是把后端真正会做的事说清楚：
// 分组里的账号**不会**被删掉，它们回落到默认分组
// （GroupService.Delete 先 MoveAccountsToGroup 再删）。
//
// 不写明白的话，用户看到「删除分组」只会想到一件事：里面那几百个账号没了。
// 于是要么不敢删，要么删了之后花半小时确认账号还在。
export function GroupDeleteDialog({ tenantID, group, onClose, onDeleted }: GroupDeleteDialogProps) {
  const { error, pending, run } = useAsyncAction();

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <LayerCard className="w-full max-w-md p-5">
        <h2 className="text-lg font-semibold text-kumo-strong">删除「{group.name}」？</h2>

        <ul className="mt-4 space-y-2 text-sm text-kumo-default">
          {group.account_count > 0 ? (
            <li>
              其中的 <b>{group.account_count}</b> 个邮箱账号会移到「默认分组」，
              <b>不会被删除</b>，凭据也不受影响。
            </li>
          ) : (
            <li>这个分组下没有邮箱账号。</li>
          )}
          <li className="text-kumo-subtle">分组本身的代理配置会随分组一起消失。</li>
        </ul>

        {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}

        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button
            type="button"
            variant="destructive"
            disabled={pending}
            onClick={() =>
              void run(async () => {
                await mailApi.deleteGroup(tenantID, group.id);
                onDeleted();
              })
            }
          >
            {pending ? "删除中…" : "删除分组"}
          </Button>
        </div>
      </LayerCard>
    </div>
  );
}
