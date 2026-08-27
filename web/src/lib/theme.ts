// 主题切换。
//
// Kumo 的颜色靠 CSS 的 light-dark() 解析，而它取的是 color-scheme：
// Kumo 自己声明的是 `:root { color-scheme: light }` 与
// `[data-mode="dark"] { color-scheme: dark }`——也就是说暗色是**显式开关**，
// 没人给根元素挂上 data-mode 的话，无论系统是什么主题都只会是亮色。
// 这就是「写了一堆语义令牌但暗色模式永远不生效」的原因。

const STORAGE_KEY = "emailbox.theme";

export type ThemePreference = "system" | "light" | "dark";
export type ResolvedTheme = "light" | "dark";

const darkQuery = () => window.matchMedia("(prefers-color-scheme: dark)");

function storedPreference(): ThemePreference {
  const raw = localStorage.getItem(STORAGE_KEY);
  return raw === "light" || raw === "dark" || raw === "system" ? raw : "system";
}

function resolveTheme(preference: ThemePreference): ResolvedTheme {
  if (preference === "system") {
    return darkQuery().matches ? "dark" : "light";
  }
  return preference;
}

function apply(theme: ResolvedTheme) {
  document.documentElement.dataset.mode = theme;
}

// initTheme 在渲染前调用一次：读偏好、挂 data-mode，并在系统主题变化时跟随
// （仅当用户没有手动指定过）。返回当前偏好，供 UI 初始化开关状态。
export function initTheme(): ThemePreference {
  const preference = storedPreference();
  apply(resolveTheme(preference));
  darkQuery().addEventListener("change", () => {
    if (storedPreference() === "system") {
      apply(resolveTheme("system"));
    }
  });
  return preference;
}

// currentTheme 返回此刻实际生效的主题。读 data-mode 而不是重新算一遍偏好：
// 用户可能存过 dark 而系统是 light，重算会得到相反的值，
// 于是开关的第一次点击看起来「没反应」。
export function currentTheme(): ResolvedTheme {
  return document.documentElement.dataset.mode === "dark" ? "dark" : "light";
}

// setTheme 记住用户的选择并立即生效。"system" 表示交回给系统偏好。
export function setTheme(preference: ThemePreference): ResolvedTheme {
  if (preference === "system") {
    localStorage.removeItem(STORAGE_KEY);
  } else {
    localStorage.setItem(STORAGE_KEY, preference);
  }
  const resolved = resolveTheme(preference);
  apply(resolved);
  return resolved;
}
