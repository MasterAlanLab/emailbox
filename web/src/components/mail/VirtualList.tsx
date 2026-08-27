import { useVirtualizer } from "@tanstack/react-virtual";
import { useRef } from "react";

// Kumo 没有虚拟化组件，这是 06 文档列出的三个自建组件之一。
// 账号列表可以有上万行，全量渲染会让滚动直接卡死。

interface VirtualListProps<T> {
  items: T[];
  // rowHeight 是行高的估计值。行高固定时给准确值即可；
  // 估不准只会多渲染几行，不影响正确性。
  rowHeight: number;
  // overscan 是可视区外预渲染的行数，用来盖住快速滚动时的白屏。
  overscan?: number;
  renderRow: (item: T, index: number) => React.ReactNode;
  className?: string;
  emptyText?: string;
}

// eslint 会对下面的 useVirtualizer 报一条 incompatible-library 警告：
// React Compiler 无法安全地记忆化它返回的函数，因此会跳过对本组件的自动记忆化。
// 这是 TanStack Virtual 与 React Compiler 的已知互操作限制，不是缺陷——
// 组件行为正确，只是少了一层编译器优化。行级的重渲染已由 AccountRow
// 只订阅自身选中状态来控制，所以实际影响可以忽略。
export function VirtualList<T>({
  items,
  rowHeight,
  overscan = 8,
  renderRow,
  className,
  emptyText,
}: VirtualListProps<T>) {
  const scrollRef = useRef<HTMLDivElement>(null);
  // 上面那段注释解释了为什么这个 warning 可以接受。留着它会让 `make lint` 每次都有噪音，
  // 而噪音一多，真正该看的新警告就没人看了——所以在这一行明确关掉。
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count: items.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => rowHeight,
    overscan,
  });

  if (items.length === 0 && emptyText) {
    return <p className="p-6 text-sm text-kumo-subtle">{emptyText}</p>;
  }

  return (
    <div ref={scrollRef} className={className} style={{ overflowY: "auto" }}>
      {/* 外层撑出总高度，让原生滚动条的比例正确；
          内层只渲染可视区的行，用 transform 定位到正确位置。 */}
      <div style={{ height: virtualizer.getTotalSize(), position: "relative" }}>
        {virtualizer.getVirtualItems().map((virtualRow) => (
          <div
            key={virtualRow.key}
            data-index={virtualRow.index}
            ref={virtualizer.measureElement}
            style={{
              position: "absolute",
              top: 0,
              left: 0,
              width: "100%",
              transform: `translateY(${virtualRow.start}px)`,
            }}
          >
            {renderRow(items[virtualRow.index], virtualRow.index)}
          </div>
        ))}
      </div>
    </div>
  );
}
