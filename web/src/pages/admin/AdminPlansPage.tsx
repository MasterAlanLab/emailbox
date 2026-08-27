import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useCallback, useEffect, useState } from "react";
import { adminApi, type Plan } from "@/api/admin";
import { AdminShell } from "@/components/admin/AdminShell";
import { useAsyncAction } from "@/lib/useAsyncAction";

// 数值型配额项。-1 表示不限，与后端 model.Unlimited 对齐。
const NUMERIC = [
  { key: "max_accounts", label: "邮箱数" },
  { key: "max_groups", label: "分组数" },
  { key: "daily_mail_fetch", label: "每日拉信" },
  { key: "daily_token_refresh", label: "每日刷新" },
] as const;

export default function AdminPlansPage() {
  const [plans, setPlans] = useState<Plan[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [reloadToken, setReloadToken] = useState(0);
  const [draft, setDraft] = useState({ code: "", name: "" });
  const { error, pending, run } = useAsyncAction();

  const reload = useCallback(() => setReloadToken((v) => v + 1), []);

  useEffect(() => {
    let ignore = false;
    void (async () => {
      try {
        const resp = await adminApi.plans();
        if (!ignore) setPlans(resp.data);
      } catch (e) {
        if (!ignore) setLoadError(e instanceof Error ? e.message : "加载失败");
      } finally {
        if (!ignore) setLoading(false);
      }
    })();
    return () => {
      ignore = true;
    };
  }, [reloadToken]);

  function create(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      await adminApi.createPlan({ code: draft.code, name: draft.name });
      setDraft({ code: "", name: "" });
      reload();
    });
  }

  const patch = (plan: Plan, data: Partial<Plan>) =>
    void run(async () => {
      // 后端不接受改 code，这里把整行原值带上再覆盖要改的那几项，
      // 免得没填的字段被后端当成 0。
      await adminApi.updatePlan(plan.id, { ...plan, ...data });
      reload();
    });

  return (
    <AdminShell title="套餐" description="配额的基线值。租户可以在此之上有单独的覆盖值。">
      <LayerCard render={<form onSubmit={create} />} className="mb-6 flex flex-wrap gap-3 p-4">
        <Input
          className="max-w-40"
          placeholder="code（如 pro）"
          value={draft.code}
          onChange={(e) => setDraft((d) => ({ ...d, code: e.target.value }))}
          required
        />
        <Input
          className="max-w-48"
          placeholder="名称（如 专业版）"
          value={draft.name}
          onChange={(e) => setDraft((d) => ({ ...d, name: e.target.value }))}
          required
        />
        <Button type="submit" variant="primary" disabled={pending}>
          新建套餐
        </Button>
        <span className="self-center text-xs text-kumo-subtle">
          新建后各项配额取数据库默认值，可在下方逐项调整。
        </span>
      </LayerCard>

      {(error || loadError) && (
        <p className="mb-4 text-sm text-kumo-danger">{error || loadError}</p>
      )}
      {loading && <p className="text-sm text-kumo-subtle">加载中…</p>}

      <div className="flex flex-col gap-4">
        {plans.map((plan) => (
          <LayerCard key={plan.id} className="p-4">
            <div className="mb-3 flex flex-wrap items-center gap-3">
              <h2 className="font-medium text-kumo-strong">
                {plan.name}
                <span className="ml-2 text-xs text-kumo-subtle">{plan.code}</span>
              </h2>
              {plan.is_default ? (
                <span className="rounded bg-kumo-tint px-2 py-0.5 text-xs">默认套餐</span>
              ) : (
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={pending}
                  onClick={() => patch(plan, { is_default: true })}
                >
                  设为默认
                </Button>
              )}
              <div className="ml-auto">
                <Button
                  size="sm"
                  variant="secondary-destructive"
                  // 默认套餐与在用套餐后端都会拒绝删除，这里不提前禁用按钮：
                  // 让用户看到那条「仍有租户在使用该套餐」的原因，比按钮变灰更有用。
                  disabled={pending}
                  onClick={() =>
                    void run(async () => {
                      await adminApi.deletePlan(plan.id);
                      reload();
                    })
                  }
                >
                  删除
                </Button>
              </div>
            </div>

            <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
              {NUMERIC.map((field) => (
                <label key={field.key} className="flex flex-col gap-1 text-xs text-kumo-subtle">
                  {field.label}
                  <Input
                    type="number"
                    defaultValue={plan[field.key]}
                    onBlur={(e) => {
                      const next = Number(e.target.value);
                      if (next !== plan[field.key]) patch(plan, { [field.key]: next });
                    }}
                  />
                </label>
              ))}
            </div>
          </LayerCard>
        ))}
      </div>
    </AdminShell>
  );
}
