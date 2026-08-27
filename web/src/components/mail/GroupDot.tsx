import type { GroupColor } from "@/api/mail";

// 分组的颜色圆点。颜色是分组唯一的视觉标识，左栏、管理页、表单预览三处共用。
//
// 类名必须写成完整字面量。Tailwind v4 靠扫描源码文本决定生成哪些工具类，
// `bg-ebx-group-${color}` 这种拼接它看不见，运行时就是一个透明的点——
// 而且 dev 有 JIT 兜底，这类问题只在生产构建里露出来（同 StatusDot）。
const COLOR_CLASS: Record<GroupColor, string> = {
  blue: "bg-ebx-group-blue",
  green: "bg-ebx-group-green",
  amber: "bg-ebx-group-amber",
  red: "bg-ebx-group-red",
  purple: "bg-ebx-group-purple",
  gray: "bg-ebx-group-gray",
};

export function GroupDot({ color, className }: { color: GroupColor; className?: string }) {
  return (
    <span
      className={["inline-block size-2.5 shrink-0 rounded-full", COLOR_CLASS[color], className]
        .filter(Boolean)
        .join(" ")}
      aria-hidden
    />
  );
}
