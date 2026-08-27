import type { Icon } from "@phosphor-icons/react";

// 居中空状态。目标视觉里每个面板的空态都是「大号灰图标 + 一句话」，
// 而不是我们现在这样在左上角丢一行灰字——面板一大，那行字就没人看得见。

interface EmptyStateProps {
  icon: Icon;
  title: string;
  // description 只在需要给出下一步动作时才写。空态文案越短越好：
  // 用户是来找东西的，不是来读说明的。
  description?: string;
  // action 放一个按钮/链接，用来把用户从空状态里带走（如「去添加邮箱」）。
  action?: React.ReactNode;
  className?: string;
}

export function EmptyState({
  icon: IconComponent,
  title,
  description,
  action,
  className,
}: EmptyStateProps) {
  return (
    // min-h-0 不能少：这个组件几乎总是放在 flex-1 的列里，
    // 缺了它内容会把外层撑破，滚动条跑到应用外壳上（09 文档 §7.2）。
    <div
      className={[
        "flex min-h-0 flex-1 flex-col items-center justify-center gap-3 p-8 text-center",
        className,
      ]
        .filter(Boolean)
        .join(" ")}
    >
      <IconComponent size={48} weight="duotone" className="text-kumo-inactive" aria-hidden />
      <p className="text-sm font-medium text-kumo-subtle">{title}</p>
      {description && <p className="max-w-xs text-xs text-kumo-subtle">{description}</p>}
      {action}
    </div>
  );
}
