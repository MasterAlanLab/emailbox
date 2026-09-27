import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { ShieldCheck, Trash } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { isTerminal, jobApi, type HealthStats } from "@/api/jobs";
import type { MailGroupNode, TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useJobStore } from "@/store/jobStore";
import { errorKindLabel } from "./errorKinds";
import { groupSelectItems } from "./groupOptions";
import { StatTile } from "./StatTile";

const JOB_TYPE = "account_check";

interface AccountHealthPanelProps {
  tenant: TenantRef;
  tenantKey: string;
  groups: MailGroupNode[];
  /** 任意任务在跑时禁用提交：刷新与检测共用一条进度流，同时只能看一个。 */
  running: boolean;
}

// 账号检测：令牌换得出来不代表邮箱能登录。线上真有一批账号令牌刷新永远成功，
// 登录邮箱却被拒（微软锁定或停用了账号），只看令牌的话它们永远显示「有效」。
// 这里的检测会真正登录一次邮箱，并据此给出「删除失效账号」的入口。
export function AccountHealthPanel({
  tenant,
  tenantKey,
  groups,
  running,
}: AccountHealthPanelProps) {
  const [stats, setStats] = useState<HealthStats | null>(null);
  const [groupIDs, setGroupIDs] = useState<string[]>([]);
  const [confirming, setConfirming] = useState(false);
  const [notice, setNotice] = useState("");
  // 删除成功后要重取数字；用自增 key 触发，而不是在回调里手算剩余数量。
  const [statsKey, setStatsKey] = useState(0);
  const { error, pending, run, setError } = useAsyncAction();
  const jobStatus = useJobStore((s) => s.status);

  useEffect(() => {
    if (!tenantKey) return undefined;
    let ignore = false;
    void jobApi
      .health(tenant)
      .then((resp) => {
        if (ignore) return;
        setStats(resp.data);
        // 与刷新任务一样：回到页面时接上仍在跑的检测。已有别的任务在看就不抢。
        const last = resp.data.last_job;
        const current = useJobStore.getState();
        const idle = current.jobID === null || (current.status && isTerminal(current.status));
        if (last && !isTerminal(last.status) && idle) {
          current.watch(tenant, last.id, JOB_TYPE);
        }
      })
      .catch((e: unknown) => {
        if (!ignore) setError(e instanceof Error ? e.message : "加载失败");
      });
    return () => {
      ignore = true;
    };
    // tenant 每次渲染都是新对象，用 tenantKey 代表它；jobStatus 让任务结束时数字跟着更新。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantKey, jobStatus, statsKey]);

  const submit = (scope: "all" | "invalid" | "group") =>
    void run(async () => {
      setNotice("");
      const resp = await jobApi.submitCheck(tenant, {
        scope,
        group_ids: scope === "group" && groupIDs.length > 0 ? groupIDs : undefined,
      });
      useJobStore.getState().watch(tenant, resp.data.id, JOB_TYPE);
    });

  const deleteInvalid = (expected: number) =>
    void run(async () => {
      try {
        const resp = await jobApi.deleteInvalid(tenant, expected);
        setNotice(`已删除 ${resp.data.deleted} 个失效账号`);
      } finally {
        // 无论成败都重取：409 说明数量变了，用户需要看到新的数字再确认。
        setConfirming(false);
        setStatsKey((k) => k + 1);
      }
    });

  const busy = pending || running;
  const invalid = stats?.invalid ?? 0;

  return (
    <LayerCard className="mb-6 p-4">
      <h2 className="text-sm font-medium text-kumo-strong">账号检测</h2>
      <p className="mt-1 text-xs text-kumo-subtle">
        实际登录一次邮箱，找出被封禁、被锁定停用或授权失效的账号。网络、代理或限流导致的失败记为「检测异常」，不算失效，也不会被删除。
      </p>

      {stats && (
        <div className="mt-4 grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile label="可用" value={stats.ok} tone="success" />
          <StatTile label="失效" value={stats.invalid} tone="danger" />
          <StatTile label="检测异常" value={stats.error} />
          <StatTile label="未检测" value={stats.unknown} />
        </div>
      )}

      <div className="mt-4 flex flex-wrap items-center gap-3">
        <Button
          variant="secondary"
          icon={ShieldCheck}
          disabled={busy}
          onClick={() => submit("all")}
        >
          检测全部
        </Button>
        <Button
          variant="secondary"
          disabled={busy || invalid === 0}
          onClick={() => submit("invalid")}
        >
          复核失效账号{invalid ? `（${invalid}）` : ""}
        </Button>
        {groups.length > 0 && (
          <div className="flex items-center gap-2">
            <span className="mx-1 h-5 w-px shrink-0 bg-kumo-line" aria-hidden />
            <Select
              className="w-44"
              aria-label="要检测的分组"
              placeholder="选择分组…"
              multiple
              items={groupSelectItems(groups, { counts: true })}
              value={groupIDs}
              onValueChange={(value: string[]) => setGroupIDs(value)}
            />
            <Button
              variant="secondary"
              disabled={busy || groupIDs.length === 0}
              onClick={() => submit("group")}
            >
              检测选中分组
            </Button>
          </div>
        )}
        <Button
          variant="secondary-destructive"
          icon={Trash}
          disabled={busy || invalid === 0}
          onClick={() => setConfirming(true)}
        >
          删除失效账号{invalid ? `（${invalid}）` : ""}
        </Button>
      </div>

      {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}
      {notice && <p className="mt-3 text-sm text-kumo-success">{notice}</p>}

      {stats && invalid > 0 && <InvalidBreakdown byKind={stats.invalid_by_kind} />}

      {confirming && stats && (
        <DeleteInvalidDialog
          stats={stats}
          pending={pending}
          onCancel={() => setConfirming(false)}
          onConfirm={() => deleteInvalid(stats.invalid)}
        />
      )}
    </LayerCard>
  );
}

function InvalidBreakdown({ byKind }: { byKind: Record<string, number> }) {
  return (
    <div className="mt-4 flex flex-col gap-2 border-t border-kumo-hairline pt-3">
      {Object.entries(byKind)
        .sort((a, b) => b[1] - a[1])
        .map(([kind, count]) => (
          <div key={kind} className="flex items-center gap-3 text-sm">
            <span className="min-w-48">{errorKindLabel(kind)}</span>
            <span className="text-kumo-subtle">{count}</span>
          </div>
        ))}
    </div>
  );
}

interface DeleteInvalidDialogProps {
  stats: HealthStats;
  pending: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

// 确认框把「删什么、为什么删、删了会怎样」说清楚：按原因列出数量，并提醒凭据会一起清空。
// 数量随请求一并提交，服务端核对不上就拒绝——确认之后又判出的失效账号不会被顺手删掉。
function DeleteInvalidDialog({ stats, pending, onCancel, onConfirm }: DeleteInvalidDialogProps) {
  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <LayerCard className="w-full max-w-md p-5">
        <h2 className="text-lg font-semibold text-kumo-strong">
          删除 {stats.invalid} 个失效账号？
        </h2>
        <ul className="mt-4 space-y-1 text-sm text-kumo-default">
          {Object.entries(stats.invalid_by_kind).map(([kind, count]) => (
            <li key={kind}>
              {errorKindLabel(kind)}：{count} 个
            </li>
          ))}
        </ul>
        <p className="mt-3 text-sm text-kumo-subtle">
          删除后账号与保存的凭据一并清除，无法恢复。需要留底请先导出。检测异常与未检测的账号不受影响。
        </p>
        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onCancel}>
            取消
          </Button>
          <Button
            type="button"
            variant="secondary-destructive"
            disabled={pending}
            onClick={onConfirm}
          >
            {pending ? "删除中…" : "确认删除"}
          </Button>
        </div>
      </LayerCard>
    </div>
  );
}
