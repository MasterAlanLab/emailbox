import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import { adminApi, type AdminPlanPrice, type Plan } from "@/api/admin";
import { useAsyncAction } from "@/lib/useAsyncAction";

// 与后端 subscriptionCurrencies 一致：Waffo 订阅产品只支持这几种。
const CURRENCIES = ["USD", "EUR", "GBP", "HKD", "JPY"].map((c) => ({ label: c, value: c }));
const PERIODS = [
  { value: "monthly", label: "月付" },
  { value: "yearly", label: "年付" },
] as const;
const SYNC_LABEL: Record<AdminPlanPrice["sync_status"], string> = {
  pending: "待同步",
  active: "已同步",
  error: "同步失败",
  inactive: "已停用",
};

interface PlanPricesProps {
  plan: Plan;
  prices: AdminPlanPrice[];
  defaultCurrency: string;
  onChanged: () => void;
}

// 一个套餐的月付 / 年付价格，放在该套餐的卡片里：价格说的就是这份配额卖多少钱，
// 分开摆在页面另一处的话，改额度和改价格要来回对照是哪一行。
export function PlanPrices({ plan, prices, defaultCurrency, onChanged }: PlanPricesProps) {
  const { error, pending, run } = useAsyncAction();
  const act = (fn: () => Promise<unknown>) =>
    void run(async () => {
      await fn();
      onChanged();
    });

  return (
    <div className="mt-4 border-t border-kumo-hairline pt-3">
      <h3 className="text-xs text-kumo-subtle">
        价格（保存后自动同步到 Waffo；改价生成新版本，已有订阅按原价续费；已同步的价格不能改币种）
      </h3>
      {PERIODS.map((period) => {
        const current = prices.find((p) => p.billing_period === period.value);
        return (
          <PriceRow
            // 重取后按最新记录重置输入框，而不是在渲染期间 setState。
            key={`${period.value}-${current?.id ?? "new"}-${current?.amount ?? ""}`}
            plan={plan}
            periodLabel={period.label}
            current={current}
            defaultCurrency={defaultCurrency}
            pending={pending}
            onSave={(amount, currency) =>
              act(() =>
                current
                  ? adminApi.updateBillingPrice(current.id, { amount, currency })
                  : adminApi.createBillingPrice({
                      plan_id: plan.id,
                      billing_period: period.value,
                      amount,
                      currency,
                    }),
              )
            }
            onToggleActive={(p) =>
              act(() => adminApi.updateBillingPrice(p.id, { active: !p.active }))
            }
            onSync={(p) => act(() => adminApi.syncBillingPrice(p.id))}
          />
        );
      })}
      {error && <p className="mt-2 text-sm text-kumo-danger">{error}</p>}
    </div>
  );
}

interface PriceRowProps {
  plan: Plan;
  periodLabel: string;
  current: AdminPlanPrice | undefined;
  defaultCurrency: string;
  pending: boolean;
  onSave: (amount: string, currency: string) => void;
  onToggleActive: (price: AdminPlanPrice) => void;
  onSync: (price: AdminPlanPrice) => void;
}

function PriceRow({
  plan,
  periodLabel,
  current,
  defaultCurrency,
  pending,
  onSave,
  onToggleActive,
  onSync,
}: PriceRowProps) {
  const [amount, setAmount] = useState(current?.amount ?? "");
  const [currency, setCurrency] = useState(current?.currency ?? defaultCurrency);
  const synced = !!current?.provider_product_id;
  return (
    <div className="flex flex-wrap items-center gap-3 py-2">
      <span className="w-12 text-sm">{periodLabel}</span>
      <Input
        className="w-28"
        size="sm"
        inputMode="decimal"
        placeholder="金额，如 9.90"
        aria-label={`${plan.name}${periodLabel}金额`}
        value={amount}
        onChange={(e) => setAmount(e.target.value.trim())}
      />
      <Select
        className="w-24"
        size="sm"
        aria-label={`${plan.name}${periodLabel}币种`}
        items={CURRENCIES}
        value={currency}
        disabled={synced}
        onValueChange={(v: string | null) => setCurrency(v ?? defaultCurrency)}
      />
      <Button
        size="sm"
        variant="secondary"
        disabled={
          pending || !amount || (amount === current?.amount && currency === current?.currency)
        }
        onClick={() => onSave(amount, currency)}
      >
        {current ? "保存" : "新增"}
      </Button>
      {current && (
        <>
          <span
            className={`text-xs ${current.sync_status === "error" ? "text-kumo-danger" : "text-kumo-subtle"}`}
            title={current.sync_error}
          >
            {SYNC_LABEL[current.sync_status]}
            {current.sync_status === "error" && current.sync_error ? `：${current.sync_error}` : ""}
          </span>
          {current.sync_status !== "active" && (
            <Button
              size="sm"
              variant="secondary"
              disabled={pending}
              onClick={() => onSync(current)}
            >
              重试同步
            </Button>
          )}
          <Button
            size="sm"
            variant={current.active ? "secondary-destructive" : "secondary"}
            disabled={pending}
            onClick={() => onToggleActive(current)}
          >
            {current.active ? "下架" : "上架"}
          </Button>
        </>
      )}
    </div>
  );
}
