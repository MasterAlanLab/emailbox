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
  /**
   * 行尾的操作入口（分组行的 ⋯ 菜单）。它是**按钮的兄弟节点**而不是子节点：
   * 嵌在里面就是 button 套 button，HTML 不合法，点它还会连带触发整行的筛选。
   */
  trailing?: React.ReactNode;
}

export function SidebarRow({
  label,
  count,
  selected,
  onSelect,
  leading,
  indent = 0,
  trailing,
}: SidebarRowProps) {
  return (
    <div className="group/row relative">
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
          <span
            className={[
              "shrink-0 text-xs text-kumo-subtle tabular-nums",
              // 操作入口和计数占同一个位置，悬停时计数让位——两者并排会把
              // 本就只有 224px 的行挤到标签没法读。
              trailing
                ? "group-hover/row:invisible group-focus-within/row:invisible group-has-[[aria-expanded=true]]/row:invisible"
                : "",
            ].join(" ")}
          >
            {count}
          </span>
        )}
      </button>

      {/* 平时透明且不可点：一个看不见却能点的按钮，会让用户在行尾莫名其妙
          打开一个菜单。菜单展开期间焦点在 portal 里，靠 aria-expanded 兜住。 */}
      {trailing && (
        <span className="pointer-events-none absolute inset-y-0 right-1 flex items-center opacity-0 group-hover/row:pointer-events-auto group-hover/row:opacity-100 group-focus-within/row:pointer-events-auto group-focus-within/row:opacity-100 group-has-[[aria-expanded=true]]/row:pointer-events-auto group-has-[[aria-expanded=true]]/row:opacity-100">
          {trailing}
        </span>
      )}
    </div>
  );
}
