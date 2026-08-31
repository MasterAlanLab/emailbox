import type { GroupColor } from "@/api/mail";

// 后端把颜色存成受限的令牌名（不是十六进制色值），这里映射到 Kumo Badge 的 variant。
// 两边的取值集合不完全一致，缺的两个在这里对齐：amber → orange、gray → neutral。
const VARIANTS: Record<GroupColor, string> = {
  blue: "blue",
  green: "green",
  amber: "orange",
  red: "red",
  purple: "purple",
  gray: "neutral",
};

export function badgeVariant(color: GroupColor): string {
  return VARIANTS[color] ?? "neutral";
}
