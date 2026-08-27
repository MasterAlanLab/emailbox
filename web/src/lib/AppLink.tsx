import type { LinkComponentProps } from "@cloudflare/kumo/utils";
import { forwardRef } from "react";
import { Link } from "react-router-dom";

// Kumo 的 Link / LinkButton 只认 href，路由跳转要靠 LinkProvider 注入的桥接组件。
// 不接这个桥，站内跳转会退化成整页刷新（丢掉 SPA 状态，还闪一下白屏）。
//
// LinkButton 会同时传 href 和 to，所以两个都读；外链与锚点走原生 <a>，
// react-router 的 Link 处理跨源 URL 时行为并不一致。
export const AppLink = forwardRef<HTMLAnchorElement, LinkComponentProps>(function AppLink(
  { href, to, ...rest },
  ref,
) {
  const target = to ?? href ?? "";
  const isExternal = /^[a-z][a-z0-9+.-]*:/i.test(target) || target.startsWith("//");
  if (isExternal || target.startsWith("#") || target === "") {
    return <a ref={ref} href={target || undefined} {...rest} />;
  }
  return <Link ref={ref} to={target} {...rest} />;
});
