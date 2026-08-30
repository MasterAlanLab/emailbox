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

const FAILURE_KIND_LABEL: Record<string, string> = {
  banned: "账号被封禁",
  auth_failed: "认证失败",
  consent_required: "权限不足",
  proxy_failed: "代理不可用",
  network: "网络不可达",
  rate_limited: "请求被限流",
  folder_unavailable: "邮箱文件夹不可用",
  provider_error: "服务商或应用配置错误",
  canceled: "刷新已取消",
};

export function ReauthorizationPanel({ tenant, tenantKey, refreshKey }: ReauthorizationPanelProps) {
  const [accounts, setAccounts] = useState<MailAccount[]>([]);
  const [selected, setSelected] = useState<MailAccount | null>(null);
  const [message, setMessage] = useState("");
  const [searchParams, setSearchParams] = useSearchParams();
  const [localRefresh, setLocalRefresh] = useState(0);
  const callbackError = searchParams.get("oauth_error") ?? "";

  useEffect(() => {
    let ignore = false;
    void loadFailedOAuthAccounts(tenant)
      .then((response) => {
        if (!ignore) setAccounts(response);
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
        <h2 className="mb-1 text-sm font-medium text-kumo-strong">刷新失败的账号</h2>
        <p className="mb-3 text-xs text-kumo-subtle">
          显示最多 200 个刷新失败的 Outlook OAuth 账号。请先查看具体原因；
          认证或权限问题可在这里重新授权，代理与应用配置问题请按提示处理。
        </p>
        {displayMessage && <p className="mb-3 text-sm text-kumo-danger">{displayMessage}</p>}
        <div className="divide-y divide-kumo-hairline">
          {accounts.map((account) => (
            <div key={account.id} className="flex items-start gap-3 py-2 text-sm">
              <div className="min-w-0 flex-1">
                <p className="truncate">{account.email}</p>
                <p className="mt-1 text-xs text-kumo-subtle">
                  {FAILURE_KIND_LABEL[account.last_refresh_error_kind] ?? "刷新失败"}
                </p>
                {account.last_refresh_error && (
                  <p className="mt-1 text-xs break-words text-kumo-danger">
                    {account.last_refresh_error}
                  </p>
                )}
              </div>
              {canReauthorize(account) && (
                <Button
                  variant="secondary"
                  size="sm"
                  icon={Key}
                  onClick={() => setSelected(account)}
                >
                  重新授权
                </Button>
              )}
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

function isOAuthFailure(account: MailAccount): boolean {
  return account.account_type === "outlook";
}

async function loadFailedOAuthAccounts(tenant: TenantRef): Promise<MailAccount[]> {
  const accounts: MailAccount[] = [];
  for (let page = 1; accounts.length < 200; page += 1) {
    const response = await mailApi.accounts(tenant, {
      refresh_status: "failed",
      page,
      limit: 200,
    });
    accounts.push(...response.data.items.filter(isOAuthFailure));
    if (page >= response.data.pagination.pages || response.data.items.length === 0) break;
  }
  return accounts.slice(0, 200);
}

function canReauthorize(account: MailAccount): boolean {
  return (
    account.last_refresh_error_kind === "auth_failed" ||
    account.last_refresh_error_kind === "consent_required" ||
    (!account.last_refresh_error_kind &&
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
