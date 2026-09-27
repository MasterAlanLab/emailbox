interface StatTileProps {
  label: string;
  value: number;
  tone?: "success" | "danger";
}

// 令牌刷新页的统计格子。刷新与检测两组数字并排出现，长相必须一致。
export function StatTile({ label, value, tone }: StatTileProps) {
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
