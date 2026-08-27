import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useEffect, useState } from "react";
import { adminApi, type AuditLog } from "@/api/admin";
import { AdminShell } from "@/components/admin/AdminShell";

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
    <AdminShell
      title="审计日志"
      description="所有写操作，以及管理员对他人数据的读操作，都会留在这里。"
    >
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Select
          className="w-40"
          size="sm"
          aria-label="按操作者筛选"
          value={actorKind}
          onValueChange={(v: string | null) => {
            setActorKind(v ?? "");
            setPage(1);
          }}
        >
          <Select.Option value="">全部操作者</Select.Option>
          <Select.Option value="admin">仅管理员</Select.Option>
          <Select.Option value="user">仅用户</Select.Option>
          <Select.Option value="system">系统</Select.Option>
        </Select>
        <Input
          className="max-w-xs"
          placeholder="动作，如 account.delete"
          value={action}
          onChange={(e) => {
            setAction(e.target.value);
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
                {log.actor_kind === "admin" && (
                  <span className="ml-2 rounded bg-kumo-tint px-1.5 py-0.5 text-xs">管理员</span>
                )}
              </span>
              <span className="min-w-0">
                <span className="font-mono text-xs">{log.action}</span>
                {log.resource_id && (
                  <span className="ml-2 truncate text-xs text-kumo-subtle">{log.resource_id}</span>
                )}
                {log.details !== "{}" && (
                  <span className="ml-2 text-xs text-kumo-subtle">{log.details}</span>
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
    </AdminShell>
  );
}
