import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useEffect, useState } from "react";
import { adminApi, type AuditLog } from "@/api/admin";
import { PageShell } from "@/components/layout/PageShell";
import { ACTOR_KIND_LABEL, AUDIT_ACTION_LABEL, auditActionLabel } from "@/lib/auditActions";

const ACTOR_ITEMS = [
  { label: "全部操作者", value: "" },
  ...Object.entries(ACTOR_KIND_LABEL).map(([value, label]) => ({ label, value })),
];
const ACTION_ITEMS = [
  { label: "全部动作", value: "" },
  ...Object.entries(AUDIT_ACTION_LABEL).map(([value, label]) => ({ label, value })),
];

// 详情里的字段名。没列出的原样显示，至少不会丢信息。
const DETAIL_LABEL: Record<string, string> = {
  from_plan: "原套餐",
  to_plan: "新套餐",
  plan_id: "套餐",
  note: "原因",
};

// formatDetails 把 details 的 JSON 摊成「字段：值」，给人看，而不是给程序看。
function formatDetails(raw: string): string {
  try {
    const parsed = JSON.parse(raw) as Record<string, unknown>;
    return Object.entries(parsed)
      .map(([k, v]) => `${DETAIL_LABEL[k] ?? k}：${typeof v === "string" ? v : JSON.stringify(v)}`)
      .join("，");
  } catch {
    return raw;
  }
}

export default function AdminAuditPage() {
  const [actorKind, setActorKind] = useState("");
  const [action, setAction] = useState("");
  const [page, setPage] = useState(1);
  const [logs, setLogs] = useState<AuditLog[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");

  useEffect(() => {
    let ignore = false;
    void (async () => {
      setLoading(true);
      try {
        const resp = await adminApi.audit({
          actor_kind: actorKind || undefined,
          action: action || undefined,
          page,
        });
        if (ignore) return;
        setLogs(resp.data.items);
        setTotal(resp.data.pagination.total);
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
  }, [actorKind, action, page]);

  return (
    <PageShell
      title="审计日志"
      description="所有写操作，以及管理员对他人数据的读操作，都会留在这里。"
    >
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Select
          className="w-36"
          size="sm"
          aria-label="按操作者筛选"
          items={ACTOR_ITEMS}
          value={actorKind}
          onValueChange={(v: string | null) => {
            setActorKind(v ?? "");
            setPage(1);
          }}
        />
        <Select
          className="w-48"
          size="sm"
          aria-label="按动作筛选"
          items={ACTION_ITEMS}
          value={action}
          onValueChange={(v: string | null) => {
            setAction(v ?? "");
            setPage(1);
          }}
        />
        <span className="text-sm text-kumo-subtle">共 {total} 条</span>
      </div>

      {error && <p className="mb-4 text-sm text-kumo-danger">{error}</p>}

      <LayerCard>
        <div className="grid grid-cols-[10rem_1fr_1fr_8rem] gap-3 border-b border-kumo-line bg-kumo-canvas px-4 py-3 text-xs font-medium text-kumo-subtle">
          <span>时间</span>
          <span>操作者</span>
          <span>动作</span>
          <span>来源 IP</span>
        </div>
        <div className="rule-list">
          {loading && <p className="px-4 py-4 text-sm text-kumo-subtle">加载中…</p>}
          {!loading && logs.length === 0 && (
            <p className="px-4 py-4 text-sm text-kumo-subtle">没有匹配的记录。</p>
          )}
          {logs.map((log) => (
            <div
              key={log.id}
              className="grid grid-cols-[10rem_1fr_1fr_8rem] gap-3 px-4 py-3 text-sm"
            >
              <span className="text-xs text-kumo-subtle">
                {new Date(log.created_at).toLocaleString("zh-CN", { hour12: false })}
              </span>
              <span className="min-w-0 truncate">
                {/* 操作者邮箱是冗余存的：actor_user_id 在用户被删后会置空，
                    那之后只有这一列还能说明是谁做的。 */}
                {log.actor_name || log.actor_user_id || "(已删除)"}
                {log.actor_kind !== "user" && ACTOR_KIND_LABEL[log.actor_kind] && (
                  <span className="ml-2 rounded bg-kumo-tint px-1.5 py-0.5 text-xs">
                    {ACTOR_KIND_LABEL[log.actor_kind]}
                  </span>
                )}
              </span>
              <span className="min-w-0">
                {/* 原始代码挂在 title 上：排查时要拿它去对日志和代码，平时看中文就够了。 */}
                <span title={log.action}>{auditActionLabel(log.action)}</span>
                {log.details !== "{}" && (
                  <span className="block truncate text-xs text-kumo-subtle">
                    {formatDetails(log.details)}
                  </span>
                )}
              </span>
              <span className="text-xs text-kumo-subtle">{log.ip}</span>
            </div>
          ))}
        </div>
      </LayerCard>

      <div className="mt-4 flex items-center gap-3">
        <Button
          size="sm"
          variant="secondary"
          disabled={page <= 1 || loading}
          onClick={() => setPage((p) => p - 1)}
        >
          上一页
        </Button>
        <span className="text-sm text-kumo-subtle">第 {page} 页</span>
        <Button
          size="sm"
          variant="secondary"
          disabled={loading || logs.length === 0 || page * 50 >= total}
          onClick={() => setPage((p) => p + 1)}
        >
          下一页
        </Button>
      </div>
    </PageShell>
  );
}
