// OverviewStat 是平台概览用的数字卡片。alert 只用来标出「需要注意的数字」，
// 不做花哨的配色——这一页看的是数值本身。
export function OverviewStat({
  label,
  value,
  hint,
  alert,
}: {
  label: string;
  value: number | string;
  hint?: string;
  alert?: boolean;
}) {
  return (
    <div className="rounded-lg border border-kumo-line bg-kumo-base p-4">
      <p className="text-xs tracking-wide text-kumo-subtle uppercase">{label}</p>
      <p
        className={`mt-1 text-2xl font-semibold ${alert ? "text-kumo-danger" : "text-kumo-strong"}`}
      >
        {value}
      </p>
      {hint && <p className="mt-1 text-xs text-kumo-subtle">{hint}</p>}
    </div>
  );
}
