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

// GROUP_COLORS 是可供用户选择的颜色列表，与后端 CHECK 约束保持一致。
export const GROUP_COLORS: GroupColor[] = ["blue", "green", "amber", "red", "purple", "gray"];
