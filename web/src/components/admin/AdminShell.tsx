import { PageShell } from "@/components/layout/PageShell";

const TABS = [
  { to: "/admin", label: "总览", end: true },
  { to: "/admin/users", label: "用户" },
  { to: "/admin/plans", label: "套餐" },
  { to: "/admin/audit", label: "审计" },
];

interface AdminShellProps {
  title: string;
  description?: string;
  children: React.ReactNode;
}

// AdminShell 是后台各页共用的外壳。排版和间距全部交给 PageShell，
// 它自己只负责「后台有哪几个页签」这一件事。
export function AdminShell({ title, description, children }: AdminShellProps) {
  return (
    <PageShell title={title} description={description} tabs={TABS}>
      {children}
    </PageShell>
  );
}

// StatTile 是总览用的数字卡片。tone 只用来区分「需要注意的数字」，
// 不做花哨的配色——后台看的是数值本身。
export function StatTile({
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
