import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Meter } from "@cloudflare/kumo/components/meter";
import { Select } from "@cloudflare/kumo/components/select";
import { ArrowsClockwise, Stop } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { isTerminal, jobApi, type RefreshStats } from "@/api/jobs";
import { mailApi, type MailGroupNode, type MailScope, type TenantRef } from "@/api/mail";
import { PageShell } from "@/components/layout/PageShell";
import { groupSelectItems } from "@/components/mail/groupOptions";
import { ReauthorizationPanel } from "@/components/mail/ReauthorizationPanel";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useJobStore } from "@/store/jobStore";
import { useTenantStore } from "@/store/tenantStore";

// 失败原因的中文说明。三类的处置完全不同，这也是把它们分开统计的全部意义：
// banned 要换账号、auth_failed 要重新授权、proxy_failed 要查代理配置。
const ERROR_KIND_LABEL: Record<string, string> = {
  banned: "账号被封禁",
  auth_failed: "令牌失效，需重新授权",
  consent_required: "权限不足，需重新授权",
  proxy_failed: "代理不可用",
  network: "网络不可达",
  rate_limited: "被限流",
  provider_error: "服务商返回错误",
  canceled: "已取消",
};

interface TokensPageProps {
  scope?: MailScope;
}

export default function TokensPage({ scope }: TokensPageProps = {}) {
  const activeTenantID = useTenantStore((s) => s.activeTenant?.id) ?? "";
  const tenant: TenantRef = scope ?? activeTenantID;
  const tenantKey = scope ? scope.tenantID : activeTenantID;

  const [stats, setStats] = useState<RefreshStats | null>(null);
  const [groups, setGroups] = useState<MailGroupNode[]>([]);
  // null 而不是空串：Kumo/Base UI 的 Select 靠 value == null 才显示 placeholder，
  // 空串会被当成「选中了一个不存在的项」，触发器上什么都不显示。
  const [groupID, setGroupID] = useState<string | null>(null);
  const [loadError, setLoadError] = useState("");
  const { error, pending, run } = useAsyncAction();

  const job = useJobStore();
  const running = job.status !== null && !isTerminal(job.status);

  useEffect(() => {
    if (!tenantKey) return undefined;
    let ignore = false;
    void (async () => {
      try {
        const resp = await jobApi.stats(tenant);
        if (ignore) return;
        setStats(resp.data);
        // 页面刚打开就接上仍在跑的那个任务：用户刷新过页面、或者从别的页面
        // 回来时，进度必须还在——这正是把事件落库的意义。
        if (resp.data.last_job && !isTerminal(resp.data.last_job.status)) {
          useJobStore.getState().watch(tenant, resp.data.last_job.id);
        }
        setLoadError("");
      } catch (e) {
        if (!ignore) setLoadError(e instanceof Error ? e.message : "加载失败");
      }
    })();
    return () => {
      ignore = true;
    };
    // 依赖里放 tenantKey 而不是 tenant：后者是每次渲染新建的对象，会让这个
    // effect 每渲染一次就重取一次。
    //
    // job.status 也在依赖里：任务结束时四个统计数字要跟着更新。用依赖触发重取，
    // 而不是在另一个 effect 里 setState 去驱动这个 effect——那是一次没必要的
    // 级联渲染，React 19 的 set-state-in-effect 规则会（正确地）拦下来。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantKey, job.status]);

  // 分组单独取一次，不挂在上面那个 effect 上：那个 effect 每次任务状态变化都会重跑，
  // 而分组在一次刷新任务里不会变。
  useEffect(() => {
    if (!tenantKey) return undefined;
    let ignore = false;
    void mailApi
      .groups(tenant)
      .then((resp) => {
        if (!ignore) setGroups(resp.data);
      })
      // 取不到分组只是少一个「按分组刷新」的入口，不该让整页报错——
      // 「刷新全部 / 只刷失败的」两条路都还在。
      .catch(() => {});
    return () => {
      ignore = true;
    };
    // 依赖同上：tenant 是每次渲染新建的对象，用 tenantKey 代表它。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantKey]);

  const submit = (scopeName: "all" | "failed" | "group") =>
    void run(async () => {
      const resp = await jobApi.submitRefresh(tenant, {
        scope: scopeName,
        group_ids: scopeName === "group" && groupID ? [groupID] : undefined,
      });
      useJobStore.getState().watch(tenant, resp.data.id);
    });

  const stop = () =>
    void run(async () => {
      if (job.jobID) await jobApi.stop(tenant, job.jobID);
    });

  if (!tenantKey) {
    return <p className="p-8 text-sm text-kumo-subtle">正在加载…</p>;
  }

  return (
    <PageShell
      title="Token 刷新"
      description="批量确认 OAuth 账号的 refresh_token 是否仍然有效，并在服务商轮换令牌时写回新值。"
    >
      {(error || loadError) && (
        <p className="mb-4 text-sm text-kumo-danger">{error || loadError}</p>
      )}

      {stats && (
        <div className="mb-6 grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile label="总账号" value={stats.total} />
          <StatTile label="正常" value={stats.success} tone="success" />
          <StatTile label="失败" value={stats.failed} tone="danger" />
          <StatTile label="从未刷新" value={stats.never} />
        </div>
      )}

      <LayerCard className="mb-6 flex flex-wrap items-center gap-3 p-4">
        <Button
          variant="secondary"
          icon={ArrowsClockwise}
          disabled={pending || running}
          onClick={() => submit("all")}
        >
          刷新全部
        </Button>
        <Button
          variant="secondary"
          disabled={pending || running || !stats?.failed}
          onClick={() => submit("failed")}
        >
          只刷新失败的{stats?.failed ? `（${stats.failed}）` : ""}
        </Button>

        {/* 按分组刷新：账号多起来之后「全部」要跑十几分钟，而用户通常只关心
            刚导入的那一批——分组就是他们区分批次的方式。
            选择器和按钮绑在一起，选完仍需明确点一下才提交：下拉一变就发任务，
            误触的代价是几千次上游调用。 */}
        {groups.length > 0 && (
          <div className="flex items-center gap-2">
            <span className="mx-1 h-5 w-px shrink-0 bg-kumo-line" aria-hidden />
            <Select
              className="w-44"
              aria-label="要刷新的分组"
              placeholder="选择分组…"
              items={groupSelectItems(groups, { counts: true })}
              value={groupID}
              onValueChange={(value: string | null) => setGroupID(value)}
            />
            <Button
              variant="secondary"
              icon={ArrowsClockwise}
              disabled={pending || running || !groupID}
              onClick={() => submit("group")}
            >
              刷新该分组
            </Button>
          </div>
        )}

        {running && (
          <Button variant="secondary-destructive" icon={Stop} disabled={pending} onClick={stop}>
            停止
          </Button>
        )}
        <span className="text-xs text-kumo-subtle">
          没有 refresh_token 的账号（IMAP 密码账号）会被自动排除。
        </span>
      </LayerCard>

      {job.jobID && (
        <LayerCard className="mb-6 p-4">
          <Meter
            label={running ? "正在刷新" : "本次结果"}
            value={job.progress.total > 0 ? (job.progress.done / job.progress.total) * 100 : 0}
            customValue={`${job.progress.done} / ${job.progress.total}`}
          />
          <div className="mt-2 flex flex-wrap gap-4 text-sm">
            <span className="text-kumo-success">成功 {job.progress.success}</span>
            <span className="text-kumo-danger">失败 {job.progress.failed}</span>
            {job.progress.current && (
              <span className="text-kumo-subtle">当前：{job.progress.current}</span>
            )}
            {job.summary && <span className="text-kumo-subtle">{job.summary}</span>}
          </div>

          {job.recent.length > 0 && (
            <div className="mt-4 max-h-64 overflow-auto rounded-md border border-kumo-line">
              {job.recent.map((item, index) => (
                <div
                  key={`${item.email}-${index}`}
                  className="flex items-center gap-3 border-b border-kumo-hairline px-3 py-1.5 text-sm last:border-b-0"
                >
                  <span className="min-w-0 flex-1 truncate">{item.email}</span>
                  {item.status === "success" && <span className="text-kumo-success">成功</span>}
                  {item.status === "skipped" && <span className="text-kumo-subtle">已跳过</span>}
                  {item.status === "failed" && (
                    <span className="text-kumo-danger" title={item.error}>
                      {ERROR_KIND_LABEL[item.errorKind] ?? (item.errorKind || "失败")}
                    </span>
                  )}
                </div>
              ))}
            </div>
          )}
        </LayerCard>
      )}

      <ReauthorizationPanel
        tenant={tenant}
        tenantKey={tenantKey}
        refreshKey={`${job.jobID ?? ""}:${job.status ?? ""}`}
      />

      {stats && Object.keys(stats.by_error_kind).length > 0 && (
        <LayerCard className="p-4">
          <h2 className="mb-3 text-sm font-medium text-kumo-strong">最近 7 天的失败原因</h2>
          <div className="flex flex-col gap-2">
            {Object.entries(stats.by_error_kind)
              .sort((a, b) => b[1] - a[1])
              .map(([kind, count]) => (
                <div key={kind} className="flex items-center gap-3 text-sm">
                  <span className="min-w-48">{ERROR_KIND_LABEL[kind] ?? (kind || "未分类")}</span>
                  <span className="text-kumo-subtle">{count}</span>
                </div>
              ))}
          </div>
        </LayerCard>
      )}
    </PageShell>
  );
}

function StatTile({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: "success" | "danger";
}) {
  const color =
    tone === "success"
      ? "text-kumo-success"
      : tone === "danger"
        ? "text-kumo-danger"
        : "text-kumo-strong";
  return (
    <div className="rounded-lg border border-kumo-line bg-kumo-base p-4">
      <p className="text-xs tracking-wide text-kumo-subtle uppercase">{label}</p>
      <p className={`mt-1 text-2xl font-semibold ${color}`}>{value}</p>
    </div>
  );
}
