import { Button } from "@cloudflare/kumo/components/button";
import { Copy, Eye, EyeSlash } from "@phosphor-icons/react";
import { useCallback, useEffect, useState } from "react";
import { apiKeyApi, type ApiKey } from "@/api/apiKey";
import { PageShell } from "@/components/layout/PageShell";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useTenantStore } from "@/store/tenantStore";

// API 接入页：一把 Key + 它能调的接口 + 给 Agent 的 llms.txt。
//
// Key 是**只读**的，能调的就是下面这五条。这个清单在后端由
// handler.APIEndpoints 定义（llms.txt 也从那里渲染），这里是给人看的那一份：
// 多了 curl，少了参数细节——真要写代码的人会去读 llms.txt。
const ENDPOINTS = [
  { method: "GET", path: "/mail/groups", summary: "列出分组" },
  { method: "GET", path: "/mail/accounts", summary: "列出账号，可按 group_id / q / status 筛" },
  {
    method: "GET",
    path: "/mail/accounts/{accountID}/messages",
    summary: "列出邮件，folder=inbox|junk|all",
  },
  { method: "GET", path: "/mail/accounts/{accountID}/messages/{messageID}", summary: "读邮件正文" },
  {
    method: "GET",
    path: "/mail/accounts/{accountID}/messages/{messageID}/attachments/{attachmentID}",
    summary: "下载附件",
  },
];

export default function ApiPage() {
  const tenantID = useTenantStore((s) => s.activeTenant?.id) ?? "";
  const [key, setKey] = useState<ApiKey | null>(null);
  const [loading, setLoading] = useState(true);
  const [revealed, setRevealed] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [loadError, setLoadError] = useState("");
  const { error, pending, run } = useAsyncAction();

  useEffect(() => {
    if (!tenantID) return undefined;
    let ignore = false;
    void apiKeyApi
      .get(tenantID)
      .then((r) => {
        if (!ignore) setKey(r.data);
      })
      .catch((e: Error) => {
        if (!ignore) setLoadError(e.message);
      })
      .finally(() => {
        if (!ignore) setLoading(false);
      });
    return () => {
      ignore = true;
    };
  }, [tenantID]);

  const reset = useCallback(
    () =>
      void run(async () => {
        const r = await apiKeyApi.reset(tenantID);
        setKey(r.data);
        setConfirming(false);
        // 刚生成的 Key 直接亮出来：这一刻用户就是来拿它的，
        // 还要再点一次「显示」纯属多余。
        setRevealed(true);
      }),
    [run, tenantID],
  );

  const base = window.location.origin;
  const header = key ? `Authorization: Bearer ${key.token}` : "";

  return (
    <PageShell
      title="API"
      description="让脚本或 Agent 用一把 Key 读取你的邮件。只读，不能改动任何东西。"
    >
      {loadError && <p className="mb-4 text-sm text-kumo-danger">{loadError}</p>}

      <div className="grid max-w-3xl gap-6">
        <section className="rounded-lg border border-kumo-line bg-kumo-elevated p-5">
          <h2 className="text-lg font-medium">请求头</h2>

          {loading ? (
            <p className="mt-4 text-sm text-kumo-subtle">加载中…</p>
          ) : key ? (
            <>
              <div className="mt-4 flex items-center gap-2">
                <code
                  data-testid="api-key-header"
                  className="min-w-0 flex-1 overflow-x-auto rounded-md bg-kumo-tint px-3 py-2 font-mono text-xs whitespace-nowrap"
                >
                  Authorization: Bearer {revealed ? key.token : mask(key.token)}
                </code>
                <Button
                  size="sm"
                  variant="ghost"
                  icon={revealed ? EyeSlash : Eye}
                  aria-label={revealed ? "隐藏" : "显示"}
                  onClick={() => setRevealed((v) => !v)}
                />
                <Button
                  size="sm"
                  variant="ghost"
                  icon={Copy}
                  aria-label="复制请求头"
                  onClick={() => copy(header)}
                />
              </div>
              <p className="mt-2 text-xs text-kumo-subtle">
                每个工作空间一把。重置会立刻作废旧的那把，正在用它的脚本会全部收到 401。
              </p>
            </>
          ) : (
            <p className="mt-4 text-sm text-kumo-subtle">还没有 API Key。</p>
          )}

          {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}

          {/* 二次确认做成同一行的两个按钮，而不是弹窗：这一步要防的是手滑，
              不是让人重新读一遍说明。 */}
          <div className="mt-4 flex items-center gap-2">
            {confirming ? (
              <>
                <span className="text-sm">确认重置？旧 Key 立即失效。</span>
                <Button size="sm" variant="destructive" disabled={pending} onClick={reset}>
                  {pending ? "重置中…" : "确认重置"}
                </Button>
                <Button size="sm" variant="secondary" onClick={() => setConfirming(false)}>
                  取消
                </Button>
              </>
            ) : key ? (
              <Button size="sm" variant="secondary" onClick={() => setConfirming(true)}>
                重置
              </Button>
            ) : (
              !loading && (
                <Button size="sm" variant="primary" disabled={pending} onClick={reset}>
                  {pending ? "生成中…" : "生成 API Key"}
                </Button>
              )
            )}
          </div>

          <dl className="mt-5 border-t border-kumo-hairline pt-4 text-sm">
            <dt className="text-kumo-subtle">工作空间 ID</dt>
            <dd className="mt-1 flex items-center gap-2">
              <code className="min-w-0 flex-1 overflow-x-auto font-mono text-xs whitespace-nowrap">
                {tenantID}
              </code>
              <Button
                size="sm"
                variant="ghost"
                icon={Copy}
                aria-label="复制工作空间 ID"
                onClick={() => copy(tenantID)}
              />
            </dd>
          </dl>
        </section>

        <section className="rounded-lg border border-kumo-line bg-kumo-elevated p-5">
          <h2 className="text-lg font-medium">接口</h2>
          <p className="mt-1 text-xs text-kumo-subtle">
            路径前缀{" "}
            <code className="font-mono">
              {base}/api/v1/tenants/{tenantID || "{tenantID}"}
            </code>
          </p>
          <div className="mt-4 overflow-x-auto">
            <table className="w-full text-left text-sm">
              <tbody className="divide-y divide-kumo-hairline">
                {ENDPOINTS.map((e) => (
                  <tr key={e.path}>
                    <td className="py-2 pr-3 align-top font-mono text-xs text-kumo-subtle">
                      {e.method}
                    </td>
                    <td className="py-2 pr-3 align-top font-mono text-xs whitespace-nowrap">
                      {e.path}
                    </td>
                    <td className="py-2 align-top text-kumo-subtle">{e.summary}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <p className="mt-3 text-xs text-kumo-subtle">
            列邮件与读正文各扣 1 次每日取件额度，用量见「用量」页。写操作一律 403。
          </p>
        </section>

        <section className="rounded-lg border border-kumo-line bg-kumo-elevated p-5">
          <h2 className="text-lg font-medium">给 Agent</h2>
          <p className="mt-1 text-sm text-kumo-subtle">
            <a className="underline" href="/llms.txt" target="_blank" rel="noreferrer">
              /llms.txt
            </a>{" "}
            是一份公开的接入说明（不含 Key）。把下面这段贴给 Agent，它就知道该读哪里、用谁的身份。
          </p>
          <div className="mt-3 flex items-center gap-2">
            <code className="min-w-0 flex-1 overflow-x-auto rounded-md bg-kumo-tint px-3 py-2 font-mono text-xs whitespace-nowrap">
              {agentPrompt(base, tenantID)}
            </code>
            <Button
              size="sm"
              variant="ghost"
              icon={Copy}
              aria-label="复制接入说明"
              onClick={() => copy(agentPrompt(base, tenantID))}
            />
          </div>
        </section>
      </div>
    </PageShell>
  );
}

// mask 只留前缀和末四位。整串打码的话，用户没法确认「页面上这把」
// 和「脚本里配的那把」是不是同一把。
function mask(token: string) {
  return `${token.slice(0, 8)}${"•".repeat(16)}${token.slice(-4)}`;
}

function agentPrompt(base: string, tenantID: string) {
  return `读取 ${base}/llms.txt 了解接口，工作空间 ID 是 ${tenantID}，API Key 由我提供。`;
}

// copy 静默失败：非 HTTPS 或用户拒权时 clipboard 不可用，
// 为此弹一个错误提示没有意义——内容就在旁边，选中复制即可。
function copy(text: string) {
  void navigator.clipboard?.writeText(text).catch(() => {});
}
