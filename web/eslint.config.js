import js from "@eslint/js";
import globals from "globals";
import reactHooks from "eslint-plugin-react-hooks";
import reactRefresh from "eslint-plugin-react-refresh";
import tseslint from "typescript-eslint";

// Kumo 的主题体系要求只用语义令牌，颜色由 light-dark() 自动切换。
// 写原始 Tailwind 色类会锁死亮色模式，写 dark: 变体反而会破坏自动切换——
// 两者都是「本地看着没问题、切到暗色才炸」的那类问题，靠 review 守不住，
// 所以在 lint 阶段拦掉。
//
// 白名单：bg-white / bg-black / text-white / text-black / *-transparent
// 这几个是 Kumo 文档明确允许的例外（需要绝对色的场景，如遮罩、印刷样式）。
const RAW_COLOR_PALETTES = [
  "slate",
  "gray",
  "zinc",
  "neutral",
  "stone",
  "red",
  "orange",
  "amber",
  "yellow",
  "lime",
  "green",
  "emerald",
  "teal",
  "cyan",
  "sky",
  "blue",
  "indigo",
  "violet",
  "purple",
  "fuchsia",
  "pink",
  "rose",
].join("|");

// 匹配 text-zinc-500 / bg-blue-500/50 / hover:border-red-600 这类写法
const rawColorClass = String.raw`(^|\s)(\w+:)*(bg|text|border|ring|fill|stroke|from|via|to|decoration|outline|shadow|accent|caret|divide|placeholder)-(${RAW_COLOR_PALETTES})-\d{2,3}(\/\d+)?(\s|$)`;
const darkVariant = String.raw`(^|\s)dark:`;

export default tseslint.config(
  { ignores: ["dist"] },
  {
    extends: [js.configs.recommended, ...tseslint.configs.recommended],
    files: ["**/*.{ts,tsx}"],
    languageOptions: {
      ecmaVersion: 2020,
      globals: globals.browser,
    },
    plugins: {
      "react-hooks": reactHooks,
      "react-refresh": reactRefresh,
    },
    rules: {
      ...reactHooks.configs.recommended.rules,
      "react-refresh/only-export-components": ["warn", { allowConstantExport: true }],
      "no-restricted-syntax": [
        "error",
        {
          selector: `Literal[value=/${rawColorClass}/]`,
          message:
            "禁止原始 Tailwind 色类。请改用 Kumo 语义令牌（bg-kumo-base / text-kumo-default / border-kumo-line 等），否则暗色模式会失效。",
        },
        {
          selector: `TemplateElement[value.raw=/${rawColorClass}/]`,
          message:
            "禁止原始 Tailwind 色类。请改用 Kumo 语义令牌（bg-kumo-base / text-kumo-default / border-kumo-line 等），否则暗色模式会失效。",
        },
        {
          selector: `Literal[value=/${darkVariant}/]`,
          message: "禁止 dark: 变体。Kumo 通过 CSS 自定义属性自动处理暗色模式，写 dark: 会破坏它。",
        },
        {
          selector: `TemplateElement[value.raw=/${darkVariant}/]`,
          message: "禁止 dark: 变体。Kumo 通过 CSS 自定义属性自动处理暗色模式，写 dark: 会破坏它。",
        },
      ],
    },
  },
);
