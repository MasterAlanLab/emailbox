import { LinkButton } from "@cloudflare/kumo/components/button";
import { ArrowRight, Envelope } from "@phosphor-icons/react";
import { Link, useLocation } from "react-router-dom";
import { useAuthStore } from "@/store/authStore";

// 公开页（首页、登录、注册、法务）的顶栏。
//
// 登录后的页面**没有**顶栏——那里所有入口都在左侧导航栏里。这一条只服务于
// 还没进门的访客：他们身上没有导航栏，登录入口必须摆在看得见的地方。
//
// 克制到底：一个品牌标记、两个动作，56px 高，无投影，底部一条 hairline 就够了。
//
// 「登录」和「注册」是一对平级入口，各自去各自的页面，谁也不比谁更重要——
// 所以两个都用 ghost。给其中一个填色，等于替访客做了选择。
// 三个入口一律走 Kumo 的 LinkButton，不再手搓 className。

export function PublicHeader() {
  const { pathname } = useLocation();
  const authed = useAuthStore((state) => state.isAuthenticated);
  // 登录页上再放一个「登录」按钮是噪音，注册页同理。
  const onLogin = pathname === "/login";
  const onRegister = pathname === "/register";

  return (
    <header className="sticky top-0 z-40 border-b border-kumo-line bg-kumo-base/80 backdrop-blur">
      <div className="shell flex h-14 items-center justify-between">
        <Link to="/" className="flex items-center gap-2.5 text-kumo-strong">
          <span className="grid size-7 place-items-center rounded-lg bg-linear-to-br from-ebx-brand-grad-from to-ebx-brand-grad-to text-white">
            <Envelope size={15} weight="fill" />
          </span>
          <span className="text-sm font-semibold tracking-[-0.01em]">Emailbox</span>
        </Link>

        <nav className="flex items-center gap-1">
          {/* 已经登录的人看到「登录 / 注册」是件很怪的事——点下去只会被守卫
              重定向回工作台。直接给他要去的地方。 */}
          {authed ? (
            <LinkButton href="/mail" variant="secondary">
              进入邮箱
              <ArrowRight size={13} />
            </LinkButton>
          ) : (
            <>
              {!onLogin && (
                <LinkButton href="/login" variant="ghost">
                  登录
                </LinkButton>
              )}
              {!onRegister && (
                <LinkButton href="/register" variant="ghost">
                  注册
                </LinkButton>
              )}
            </>
          )}
        </nav>
      </div>
    </header>
  );
}
