// 账号状态圆点。左栏分组面板和底部状态栏都要用它，
// 两处共用同一套颜色令牌（--color-ebx-dot-*，定义在 style.css），
// 免得「在线」在一处是绿、在另一处是另一种绿。

export type StatusTone = "online" | "error" | "idle" | "unread";

// 类名必须是写死的完整字符串。Tailwind v4 靠扫描源码文本来决定生成哪些工具类，
// 拼接出来的 `bg-ebx-dot-${tone}` 它看不见，运行时就是一个没有背景色的透明点——
// 这类问题只在生产构建里出现（dev 有 JIT 兜底），最难查。
const TONE_CLASS: Record<StatusTone, string> = {
  online: "bg-ebx-dot-online",
  error: "bg-ebx-dot-error",
  idle: "bg-ebx-dot-idle",
  unread: "bg-ebx-dot-unread",
};

interface StatusDotProps {
  tone: StatusTone;
  // label 给读屏用。颜色是这个组件唯一的信息载体，
  // 不配文字说明的话，色觉障碍用户和读屏用户都拿不到这条信息。
  label?: string;
  className?: string;
}

export function StatusDot({ tone, label, className }: StatusDotProps) {
  return (
    <span
      className={["inline-block size-2 shrink-0 rounded-full", TONE_CLASS[tone], className]
        .filter(Boolean)
        .join(" ")}
      role={label ? "img" : undefined}
      aria-label={label}
      aria-hidden={label ? undefined : true}
    />
  );
}
