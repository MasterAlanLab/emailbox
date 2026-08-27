import { useCallback, useRef, useState } from "react";

// 纵向可拖拽分栏。06 文档很早就列出了这个组件，一直没建。
//
// 响应式**全部交给 CSS**，不用 matchMedia：
// 分隔条自己 `hidden md:flex`，上段的固定高度也只在 `md:` 起效。
// 窄屏下两段都是 flex-1，配合外部传进来的 hidden/flex（层级导航）自然退化成单栏——
// 用 JS 判断断点就得再处理一次「JS 的 768 和 Tailwind 的 768 是否一致」，
// 两套真源迟早对不上。

const MIN_RATIO = 0.2;
const MAX_RATIO = 0.8;
const KEY_STEP = 0.05;

interface SplitPaneProps {
  top: React.ReactNode;
  bottom: React.ReactNode;
  // 外部控制两段的显示（窄屏层级导航靠它切换），因此要能分别加 class。
  topClassName?: string;
  bottomClassName?: string;
  // storageKey 给了就记住用户拖到的位置。分栏比例是很个人的偏好，
  // 每次进页面都重置回默认值会让人反复拖同一下。
  storageKey?: string;
  defaultRatio?: number;
}

export function SplitPane({
  top,
  bottom,
  topClassName = "",
  bottomClassName = "",
  storageKey,
  defaultRatio = 0.55,
}: SplitPaneProps) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [ratio, setRatio] = useState(() => restore(storageKey) ?? defaultRatio);
  const [dragging, setDragging] = useState(false);

  const commit = useCallback(
    (next: number) => {
      const clamped = Math.min(MAX_RATIO, Math.max(MIN_RATIO, next));
      setRatio(clamped);
      if (storageKey) {
        try {
          localStorage.setItem(storageKey, String(clamped));
        } catch {
          // 隐私模式下 localStorage 会抛。记不住位置无所谓，不能因此让拖拽失败。
        }
      }
    },
    [storageKey],
  );

  const onPointerMove = useCallback(
    (event: React.PointerEvent) => {
      if (!dragging) return;
      const box = containerRef.current?.getBoundingClientRect();
      if (!box || box.height === 0) return;
      commit((event.clientY - box.top) / box.height);
    },
    [dragging, commit],
  );

  return (
    <div
      ref={containerRef}
      className="flex min-h-0 min-w-0 flex-1 flex-col"
      style={{ "--ebx-split": `${ratio * 100}%` } as React.CSSProperties}
      onPointerMove={onPointerMove}
      onPointerUp={() => setDragging(false)}
    >
      <div
        className={`${topClassName} min-h-0 flex-1 flex-col md:flex-none md:basis-(--ebx-split)`}
      >
        {top}
      </div>

      <div
        role="separator"
        aria-orientation="horizontal"
        aria-label="调整邮件列表与详情的高度"
        aria-valuenow={Math.round(ratio * 100)}
        tabIndex={0}
        className="group hidden h-1.5 shrink-0 cursor-row-resize items-center justify-center border-y border-kumo-line bg-kumo-canvas hover:bg-kumo-tint focus-visible:bg-kumo-tint md:flex"
        onPointerDown={(event) => {
          // 捕获指针：手指/鼠标划出分隔条之后还能继续收到 move。
          event.currentTarget.setPointerCapture(event.pointerId);
          setDragging(true);
        }}
        // 键盘也要能调。分隔条只能用鼠标拖的话，键盘用户没有任何办法改变布局。
        onKeyDown={(event) => {
          if (event.key === "ArrowUp") commit(ratio - KEY_STEP);
          else if (event.key === "ArrowDown") commit(ratio + KEY_STEP);
          else return;
          event.preventDefault();
        }}
      >
        <span className="h-0.5 w-8 rounded-full bg-kumo-line group-hover:bg-kumo-interact" />
      </div>

      <div className={`${bottomClassName} min-h-0 flex-1 flex-col`}>{bottom}</div>

      {/* 拖拽期间盖一层透明遮罩。下段的邮件正文是个 sandbox iframe，
          指针一旦移到它上面，事件就进了另一个文档，拖拽会「卡在半路」——
          这是分栏 + iframe 组合最典型的坑（09 文档 §7.1）。
          遮罩顺带统一了拖拽时的光标，不会因为经过不同元素而闪来闪去。 */}
      {dragging && <div className="fixed inset-0 z-50 cursor-row-resize" />}
    </div>
  );
}

function restore(storageKey?: string): number | null {
  if (!storageKey) return null;
  try {
    const raw = Number(localStorage.getItem(storageKey));
    return raw >= MIN_RATIO && raw <= MAX_RATIO ? raw : null;
  } catch {
    return null;
  }
}
