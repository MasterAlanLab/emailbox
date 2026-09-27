import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { CheckCircle } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { adminApi, type Plan, type TenantPlan } from "@/api/admin";
import { useAsyncAction } from "@/lib/useAsyncAction";

export interface AssignTarget {
  tenantID: string;
  title: string;
  subtitle: string;
}

interface AssignPlanDialogProps {
  target: AssignTarget;
  onClose: () => void;
  onAssigned: () => void;
}

// 仍在扣费的订阅状态：给这些用户手动换套餐，他在 Waffo 的订阅不会因此停止。
const BILLING_STATUS: Record<string, string> = {
  active: "生效中",
  canceling: "到期后取消",
  past_due: "续费失败",
};

const quota = (n: number) => (n < 0 ? "不限" : n.toLocaleString());

// 分配套餐：直接给用户挂上某一档套餐，不经支付。额度一律取套餐本身的值，
// 没有逐项覆盖——要多给就换一档。分配后套餐归管理员所有，订阅取消时不会被回收。
export function AssignPlanDialog({ target, onClose, onAssigned }: AssignPlanDialogProps) {
  const [plans, setPlans] = useState<Plan[]>([]);
  const [current, setCurrent] = useState<TenantPlan | null>(null);
  const [selected, setSelected] = useState("");
  const [loadError, setLoadError] = useState("");
  const { error, pending, run } = useAsyncAction();

  useEffect(() => {
    let ignore = false;
    void Promise.all([adminApi.plans(), adminApi.tenantPlan(target.tenantID)])
      .then(([planResp, tenantResp]) => {
        if (ignore) return;
        setPlans(planResp.data);
        setCurrent(tenantResp.data);
        setSelected(tenantResp.data.limits.plan_id);
      })
      .catch((e: unknown) => {
        if (!ignore) setLoadError(e instanceof Error ? e.message : "加载失败");
      });
    return () => {
      ignore = true;
    };
  }, [target.tenantID]);

  const currentID = current?.limits.plan_id ?? "";
  const billing = current?.subscription && BILLING_STATUS[current.subscription.status];

  const assign = () =>
    void run(async () => {
      await adminApi.assignPlan(target.tenantID, selected);
      onAssigned();
    });

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <LayerCard className="flex max-h-[90vh] w-full max-w-lg flex-col p-5">
        <h2 className="text-lg font-semibold text-kumo-strong">分配套餐</h2>
        <p className="mt-1 text-sm text-kumo-subtle">
          {target.title}（{target.subtitle}）
        </p>

        {current && (
          <p className="mt-3 text-sm">
            当前：{current.limits.plan_name}
            <span className="ml-1 text-kumo-subtle">
              （{current.limits.plan_source === "subscription" ? "用户订阅" : "管理员分配"}）
            </span>
          </p>
        )}
        {billing && current?.subscription && (
          <p className="mt-2 rounded-md border border-kumo-line p-3 text-sm text-kumo-warning">
            该用户有付费订阅（{billing}
            {current.subscription.current_period_end
              ? `，周期至 ${current.subscription.current_period_end.slice(0, 10)}`
              : ""}
            ）。分配后以你的选择为准，但订阅不会被取消、仍会按期扣费；要停止扣费请到 Waffo
            后台取消。
          </p>
        )}

        {loadError && <p className="mt-3 text-sm text-kumo-danger">{loadError}</p>}

        <div
          role="radiogroup"
          aria-label="套餐"
          className="mt-4 flex min-h-0 flex-col gap-2 overflow-y-auto"
        >
          {plans.map((plan) => {
            const checked = plan.id === selected;
            return (
              <button
                key={plan.id}
                type="button"
                role="radio"
                aria-checked={checked}
                onClick={() => setSelected(plan.id)}
                className={`rounded-lg border border-kumo-line p-3 text-left ${
                  checked ? "bg-kumo-tint" : "hover:bg-kumo-tint"
                }`}
              >
                <span className="flex items-center gap-2 text-sm font-medium text-kumo-strong">
                  {/* 选中态靠勾而不是边框颜色：界面上不引入 Kumo 之外的强调色。 */}
                  <CheckCircle
                    size={16}
                    weight={checked ? "fill" : "regular"}
                    className={checked ? "text-kumo-strong" : "text-kumo-subtle"}
                    aria-hidden
                  />
                  {plan.name}
                  <span className="text-xs font-normal text-kumo-subtle">{plan.code}</span>
                  {plan.is_default && <span className="text-xs text-kumo-subtle">默认</span>}
                  {plan.id === currentID && (
                    <span className="ml-auto text-xs font-normal text-kumo-subtle">当前</span>
                  )}
                </span>
                <span className="mt-1 block text-xs text-kumo-subtle">
                  邮箱 {quota(plan.max_accounts)} · 分组 {quota(plan.max_groups)} · 每日拉信{" "}
                  {quota(plan.daily_mail_fetch)}
                </span>
              </button>
            );
          })}
        </div>

        {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}

        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button
            type="button"
            variant="secondary"
            disabled={pending || !selected || selected === currentID}
            onClick={assign}
          >
            {pending ? "分配中…" : "分配"}
          </Button>
        </div>
      </LayerCard>
    </div>
  );
}
