import { Button } from "@cloudflare/kumo/components/button";
import {
  Dialog,
  DialogClose,
  DialogDescription,
  DialogRoot,
  DialogTitle,
} from "@cloudflare/kumo/components/dialog";
import { Textarea } from "@cloudflare/kumo/components/input";
import { ArrowSquareOut, X } from "@phosphor-icons/react";
import { useState } from "react";
import { mailApi, type MailAccount, type OAuthStartResult, type TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";

interface ReauthorizationDialogProps {
  account: MailAccount;
  tenant: TenantRef;
  onClose: () => void;
  onCompleted: () => void;
}

export function ReauthorizationDialog({
  account,
  tenant,
  onClose,
  onCompleted,
}: ReauthorizationDialogProps) {
  const [flow, setFlow] = useState<OAuthStartResult | null>(null);
  const [redirectedURL, setRedirectedURL] = useState("");
  const { error, pending, run } = useAsyncAction();

  const start = () =>
    void run(async () => {
      const response = await mailApi.startReauthorization(tenant, account.id);
      setFlow(response.data);
    });

  const complete = () =>
    void run(async () => {
      if (!flow) return;
      await mailApi.completeReauthorization(tenant, account.id, {
        flow_id: flow.flow_id,
        redirected_url: redirectedURL.trim(),
      });
      onCompleted();
    });

  return (
    <DialogRoot open onOpenChange={(open) => !open && onClose()}>
      <Dialog size="lg" className="p-0">
        <div className="flex items-center justify-between border-b border-kumo-line px-6 py-4">
          <DialogTitle className="text-lg font-semibold">重新授权 Microsoft 账号</DialogTitle>
          <DialogClose
            render={(props) => (
              <Button
                {...props}
                variant="secondary"
                shape="square"
                size="sm"
                aria-label="关闭"
                disabled={pending}
              >
                <X size={18} />
              </Button>
            )}
          />
        </div>

        <div className="flex flex-col gap-4 p-6">
          <DialogDescription className="text-sm text-kumo-subtle">
            账号：<span className="text-kumo-default">{account.email}</span>。新令牌验证成功前，
            当前凭据保持不变。
          </DialogDescription>

          {!flow ? (
            <div>
              <p className="mb-3 text-sm text-kumo-subtle">
                先创建一个 10 分钟内有效的一次性授权流程。
              </p>
              <Button variant="secondary" disabled={pending} onClick={start}>
                {pending ? "正在创建…" : "创建授权流程"}
              </Button>
            </div>
          ) : (
            <>
              <div className="flex flex-col gap-2">
                <p className="text-sm">1. 在新标签页登录 Microsoft，并同意邮件权限。</p>
                <Button
                  variant="secondary"
                  icon={ArrowSquareOut}
                  onClick={() =>
                    window.open(flow.authorization_url, "_blank", "noopener,noreferrer")
                  }
                >
                  打开 Microsoft 授权页
                </Button>
              </div>

              <div className="flex flex-col gap-2">
                <p className="text-sm">
                  2. 若授权后跳到 localhost 且页面未打开，复制地址栏里的完整地址并粘贴到这里。
                  配置了本项目回调域名时，页面会自动完成此步骤。
                </p>
                <Textarea
                  aria-label="授权后的完整跳转地址"
                  placeholder="http://localhost:8080/?code=...&state=..."
                  rows={4}
                  value={redirectedURL}
                  onChange={(event) => setRedirectedURL(event.target.value)}
                  disabled={pending}
                />
                <Button
                  variant="secondary"
                  disabled={pending || redirectedURL.trim() === ""}
                  onClick={complete}
                >
                  {pending ? "正在验证…" : "验证并更新令牌"}
                </Button>
              </div>
            </>
          )}

          {error && <p className="text-sm text-kumo-danger">{error}</p>}
        </div>
      </Dialog>
    </DialogRoot>
  );
}
