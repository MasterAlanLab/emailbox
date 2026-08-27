import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useEffect, useState } from "react";
import { adminApi, type Plan } from "@/api/admin";
import { useAsyncAction } from "@/lib/useAsyncAction";

// 只要「改谁的配额」这三样，不绑定某个具体的列表行类型：
// 后台的租户列表已经并进用户列表（一个租户空间只属于一个用户），
// 调用方传的是 AdminUser，将来若再有别的入口也不必再改这里。
export interface QuotaTarget {
  tenantID: string;
  title: string;
  subtitle: string;
}

interface QuotaDialogProps {
  target: QuotaTarget;
  onClose: () => void;
  onSaved: () => void;
}

// 可覆盖的配额项。留空表示「不覆盖，跟随套餐」——这与「设为 0」是两回事，
// 后者意味着彻底禁用该能力，所以输入框的空值不能当 0 处理。
const FIELDS = [
  { key: "max_accounts", label: "邮箱数上限" },
  { key: "max_groups", label: "分组数上限" },
  { key: "daily_mail_fetch", label: "每日拉信次数" },
] as const;

type FieldKey = (typeof FIELDS)[number]["key"];

export function QuotaDialog({ target, onClose, onSaved }: QuotaDialogProps) {
  const [plans, setPlans] = useState<Plan[]>([]);
  const [planID, setPlanID] = useState("");
  const [note, setNote] = useState("");
  const [values, setValues] = useState<Record<FieldKey, string>>({
    max_accounts: "",
    max_groups: "",
    daily_mail_fetch: "",
  });
  const { error, pending, run } = useAsyncAction();

  useEffect(() => {
    let ignore = false;
    void (async () => {
      const resp = await adminApi.plans();
      if (!ignore) setPlans(resp.data);
    })();
    return () => {
      ignore = true;
    };
  }, []);

  function submit(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      const overrides: Record<string, number | null> = {};
      for (const field of FIELDS) {
        const raw = values[field.key].trim();
        // 空串 = 不覆盖，发 null 让后端回落到套餐值
        overrides[field.key] = raw === "" ? null : Number(raw);
      }
      await adminApi.updateTenantQuota(target.tenantID, {
        plan_id: planID || undefined,
        note,
        ...overrides,
      });
      onSaved();
    });
  }

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <LayerCard render={<form onSubmit={submit} />} className="w-full max-w-lg overflow-auto p-5">
        <h2 className="text-lg font-semibold text-kumo-strong">调整配额</h2>
        <p className="mt-1 text-sm text-kumo-subtle">
          {target.title}（{target.subtitle}）
        </p>

        <div className="mt-4 flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            套餐
            <Select
              aria-label="套餐"
              value={planID}
              onValueChange={(v: string | null) => setPlanID(v ?? "")}
            >
              <Select.Option value="">保持不变</Select.Option>
              {plans.map((plan) => (
                <Select.Option key={plan.id} value={plan.id}>
                  {plan.name}（{plan.code}）
                </Select.Option>
              ))}
            </Select>
          </label>

          {FIELDS.map((field) => (
            <label key={field.key} className="flex flex-col gap-1 text-sm">
              {field.label}
              <Input
                type="number"
                placeholder="留空表示跟随套餐，-1 表示不限"
                value={values[field.key]}
                onChange={(e) => setValues((prev) => ({ ...prev, [field.key]: e.target.value }))}
              />
            </label>
          ))}

          <label className="flex flex-col gap-1 text-sm">
            调整原因（必填）
            <Input
              placeholder="例如：付费升级 / 疑似滥用临时收紧"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              required
            />
          </label>
          {/* 后端也强制要求 note。三个月后回看时，它是唯一能说明
              「为什么这个租户跟别人不一样」的东西。 */}
        </div>

        {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}

        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" variant="primary" disabled={pending}>
            {pending ? "保存中…" : "保存"}
          </Button>
        </div>
      </LayerCard>
    </div>
  );
}
