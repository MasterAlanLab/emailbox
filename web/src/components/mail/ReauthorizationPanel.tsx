import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Key } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { mailApi, type MailAccount, type TenantRef } from "@/api/mail";
import { ReauthorizationDialog } from "@/components/mail/ReauthorizationDialog";

interface ReauthorizationPanelProps {
  tenant: TenantRef;
  tenantKey: string;
  refreshKey: string;
}

export function ReauthorizationPanel({ tenant, tenantKey, refreshKey }: ReauthorizationPanelProps) {
  const [accounts, setAccounts] = useState<MailAccount[]>([]);
  const [selected, setSelected] = useState<MailAccount | null>(null);
  const [message, setMessage] = useState("");
  const [searchParams, setSearchParams] = useSearchParams();
  const [localRefresh, setLocalRefresh] = useState(0);
  const callbackError = searchParams.get("oauth_error") ?? "";

  useEffect(() => {
    let ignore = false;
    void mailApi
      .accounts(tenant, { refresh_status: "failed", limit: 200 })
      .then((response) => {
        if (!ignore) setAccounts(response.data.items.filter(needsReauthorization));
      })
      .catch((error: unknown) => {
        if (!ignore) setMessage(error instanceof Error ? error.message : "加载失败账号失败");
      });
    return () => {
      ignore = true;
    };
    // tenant 是每次渲染新建的 scope 对象，tenantKey 才是稳定身份。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantKey, refreshKey, localRefresh]);

  useEffect(() => {
    const flowID = searchParams.get("oauth_flow_id");
    const accountID = searchParams.get("oauth_account_id");
    const callbackTenantID = searchParams.get("oauth_tenant_id");
    if (callbackError) return;
    if (!flowID || !accountID || callbackTenantID !== tenantKey) return;
    let ignore = false;
    void mailApi
      .completeReauthorization(tenant, accountID, { flow_id: flowID })
      .then(() => {
        if (ignore) return;
        setMessage("重新授权成功");
        setLocalRefresh((value) => value + 1);
        clearOAuthParams(searchParams, setSearchParams);
      })
      .catch((error: unknown) => {
        if (!ignore) setMessage(error instanceof Error ? error.message : "重新授权失败");
      });
    return () => {
      ignore = true;
    };
    // 只在回调参数或租户变化时执行；tenant 对象不是稳定引用。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [callbackError, tenantKey, searchParams]);

  const displayMessage = callbackError || message;
  if (accounts.length === 0 && !displayMessage) return null;

  return (
    <>
      <LayerCard className="mb-6 p-4">
        <h2 className="mb-1 text-sm font-medium text-kumo-strong">需要重新授权的账号</h2>
        <p className="mb-3 text-xs text-kumo-subtle">
          显示当前页最多 200 个令牌失效或缺少权限的 Outlook 账号。
        </p>
        {displayMessage && <p className="mb-3 text-sm text-kumo-danger">{displayMessage}</p>}
        <div className="divide-y divide-kumo-hairline">
          {accounts.map((account) => (
            <div key={account.id} className="flex items-center gap-3 py-2 text-sm">
              <span className="min-w-0 flex-1 truncate">{account.email}</span>
              <span className="hidden text-kumo-subtle sm:inline">
                {account.last_refresh_error_kind === "consent_required" ? "权限不足" : "令牌失效"}
              </span>
              <Button variant="secondary" size="sm" icon={Key} onClick={() => setSelected(account)}>
                重新授权
              </Button>
            </div>
          ))}
        </div>
      </LayerCard>

      {selected && (
        <ReauthorizationDialog
          account={selected}
          tenant={tenant}
          onClose={() => setSelected(null)}
          onCompleted={() => {
            setSelected(null);
            setMessage("重新授权成功");
            setLocalRefresh((value) => value + 1);
          }}
        />
      )}
    </>
  );
}

function needsReauthorization(account: MailAccount): boolean {
  return (
    account.account_type === "outlook" &&
    (account.last_refresh_error_kind === "auth_failed" ||
      account.last_refresh_error_kind === "consent_required" ||
      /重新授权|refresh_token|令牌失效/i.test(account.last_refresh_error))
  );
}

function clearOAuthParams(
  params: URLSearchParams,
  setParams: ReturnType<typeof useSearchParams>[1],
) {
  const next = new URLSearchParams(params);
  for (const key of [
    "oauth_status",
    "oauth_flow_id",
    "oauth_tenant_id",
    "oauth_account_id",
    "oauth_error",
  ]) {
    next.delete(key);
  }
  setParams(next, { replace: true });
}
