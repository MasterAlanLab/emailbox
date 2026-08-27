import { Badge } from "@cloudflare/kumo/components/badge";
import { Banner } from "@cloudflare/kumo/components/banner";
import { Meter } from "@cloudflare/kumo/components/meter";
import { useEffect, useState } from "react";
import { tenantApi, type QuotaUsage } from "@/api/tenant";
import { PageShell } from "@/components/layout/PageShell";
import { useTenantStore } from "@/store/tenantStore";

// UNLIMITED 与后端 model.Unlimited 对应：-1 表示不限。
const UNLIMITED = -1;
// 用量达到这个比例时提示，给用户留出处理时间而不是等到已经超了才说。
const WARN_RATIO = 0.8;

export default function UsagePage() {
  const tenantID = useTenantStore((s) => s.activeTenant?.id) ?? "";
  const [data, setData] = useState<QuotaUsage | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (!tenantID) return undefined;
    let ignore = false;
    void tenantApi
      .quota(tenantID)
      .then((r) => {
        if (!ignore) setData(r.data);
      })
      .catch((e: Error) => {
        if (!ignore) setError(e.message);
      });
    return () => {
      ignore = true;
    };
  }, [tenantID]);

  if (error) {
    return (
      <PageShell title="用量">
        <p className="text-sm text-kumo-danger">{error}</p>
      </PageShell>
    );
  }
  if (!data) {
    return (
      <PageShell title="用量">
        <p className="text-sm text-kumo-subtle">加载中…</p>
      </PageShell>
    );
  }

  const { limits, usage } = data;
  const counted = [
    { label: "邮箱账号", used: usage.accounts, limit: limits.max_accounts },
    { label: "分组", used: usage.groups, limit: limits.max_groups },
  ];
  const daily = [{ label: "今日拉取邮件", used: usage.mail_fetch, limit: limits.daily_mail_fetch }];
  const overQuota = counted.filter((q) => q.limit !== UNLIMITED && q.used > q.limit);

  return (
    <PageShell title="用量" description={limits.plan_name}>
      {/* 管理员调低配额时不追溯删除已有数据，只阻止新增，所以这里可能出现「已超额」 */}
      {overQuota.length > 0 && (
        <Banner variant="alert" className="mb-6">
          {overQuota.map((q) => `${q.label}已超出上限 ${q.used - q.limit} 个`).join("；")}
          。已有数据不受影响，但无法继续新增。
        </Banner>
      )}

      <div className="grid max-w-3xl gap-6 lg:grid-cols-2">
        <QuotaCard title="资源用量" items={counted} />
        {/* 令牌刷新没有额度：它是账号能不能用的前提，卡住它等于让账号批量失效。
            既然不受限，就不摆在「额度」里占位——用量页只讲有上限的东西。 */}
        <QuotaCard title={`每日额度（${data.day} 重置）`} items={daily} />
      </div>
    </PageShell>
  );
}

function QuotaCard({
  title,
  items,
}: {
  title: string;
  items: { label: string; used: number; limit: number }[];
}) {
  return (
    <section className="rounded-lg border border-kumo-line bg-kumo-elevated p-5">
      <h2 className="text-lg font-medium">{title}</h2>
      <div className="mt-4 space-y-5">
        {items.map((item) => (
          <QuotaMeter key={item.label} {...item} />
        ))}
      </div>
    </section>
  );
}

function QuotaMeter({ label, used, limit }: { label: string; used: number; limit: number }) {
  if (limit === UNLIMITED) {
    return (
      <div className="flex items-center justify-between text-sm">
        <span>{label}</span>
        <span className="flex items-center gap-2 text-kumo-subtle">
          {used.toLocaleString()}
          <Badge variant="neutral">不限</Badge>
        </span>
      </div>
    );
  }
  // limit 为 0 时百分比无意义，直接按已满处理，免得出现 Infinity。
  const ratio = limit > 0 ? used / limit : 1;
  return (
    <div>
      <Meter
        label={label}
        value={Math.min(Math.round(ratio * 100), 100)}
        customValue={`${used.toLocaleString()} / ${limit.toLocaleString()}`}
      />
      {ratio >= WARN_RATIO && (
        <p className="mt-1 text-xs text-kumo-warning">
          {ratio >= 1 ? "已达上限，无法继续新增。" : "接近上限。"}
        </p>
      )}
    </div>
  );
}
