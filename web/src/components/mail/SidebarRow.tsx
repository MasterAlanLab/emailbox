// 左栏所有可点行共用的形状：前导标记 + 标签 + 右侧计数。
//
// 状态行和分组行长得一样，用户才会把它们读成同一层级的两组筛选，
// 而不是「一个是导航、一个是筛选」。
//
// 单独一个文件是因为 MailSidebar 和 GroupList 都要用它：
// 放在 MailSidebar 里会和 GroupList 形成循环 import。

interface SidebarRowProps {
  label: string;
  count?: number;
  selected: boolean;
  onSelect: () => void;
  leading?: React.ReactNode;
  indent?: number;
}

export function SidebarRow({
  label,
  count,
  selected,
  onSelect,
  leading,
  indent = 0,
}: SidebarRowProps) {
  return (
    <button
      type="button"
      onClick={onSelect}
      aria-current={selected ? "true" : undefined}
      style={{ paddingLeft: `${indent * 14 + 8}px` }}
      className={[
        "flex min-h-8 w-full items-center gap-2 rounded-lg pr-2 text-left text-sm",
        selected
          ? "bg-kumo-tint font-medium text-kumo-strong"
          : "text-kumo-default hover:bg-kumo-interact",
      ].join(" ")}
    >
      {leading}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {count !== undefined && (
        <span className="shrink-0 text-xs text-kumo-subtle tabular-nums">{count}</span>
      )}
    </button>
  );
}
