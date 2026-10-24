import { LinkButton } from "@cloudflare/kumo/components/button";
import { ArrowRight, Envelope } from "@phosphor-icons/react";
import { Link, NavLink, useLocation } from "react-router-dom";
import { SUPPORT_EMAIL } from "@/lib/support";
import { useAuthStore } from "@/store/authStore";

const textLinkClassName =
  "rounded-sm py-2 text-kumo-subtle transition-colors hover:text-kumo-strong focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-kumo-focus";

export function PublicHeader() {
  const { pathname } = useLocation();
  const authed = useAuthStore((state) => state.isAuthenticated);
  const loading = useAuthStore((state) => state.loading);
  // 登录页上再放一个「登录」按钮是噪音，注册页同理。
  const onLogin = pathname === "/login";
  const onRegister = pathname === "/register";

  return (
    <header className="sticky top-0 z-40 border-b border-kumo-line bg-kumo-base/80 backdrop-blur">
      <div className="shell flex h-16 items-center justify-between gap-4 sm:h-18">
        <Link
          to="/"
          aria-label="Emailbox 首页"
          className="flex shrink-0 items-center gap-2 rounded-sm text-kumo-strong focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-kumo-focus sm:gap-2.5"
        >
          <span className="grid size-6 place-items-center rounded-lg bg-linear-to-br from-ebx-brand-grad-from to-ebx-brand-grad-to text-white sm:size-7">
            <Envelope size={15} weight="fill" />
          </span>
          <span className="text-base font-semibold tracking-[-0.02em] sm:text-lg">Emailbox</span>
        </Link>

        <nav
          aria-label="公开导航"
          className="flex shrink-0 items-center gap-4 text-sm font-medium sm:gap-6"
        >
          <div className="flex items-center gap-6">
            <NavLink
              to="/pricing"
              className={`${textLinkClassName} aria-[current=page]:text-kumo-strong`}
            >
              价格
            </NavLink>
            <a href={`mailto:${SUPPORT_EMAIL}`} className={`hidden sm:block ${textLinkClassName}`}>
              联系支持
            </a>
          </div>
          <span aria-hidden="true" className="hidden h-4 w-px bg-kumo-line sm:block" />
          {/* 已经登录的人看到「登录 / 注册」是件很怪的事——点下去只会被守卫
              重定向回工作台。直接给他要去的地方。 */}
          <div className="flex min-h-8 items-center gap-4 sm:gap-5">
            {authed ? (
              <LinkButton
                href="/mail"
                variant="secondary"
                size="sm"
                className="h-8 rounded-lg px-3 text-sm sm:h-9"
              >
                进入控制台
                <ArrowRight size={13} />
              </LinkButton>
            ) : loading ? (
              // 等会话恢复完成再显示身份入口，避免已登录用户先看到登录 / 注册。
              <span aria-hidden="true" className="h-8 w-24" />
            ) : (
              <>
                {!onLogin && (
                  <Link to="/login" className={textLinkClassName}>
                    登录
                  </Link>
                )}
                {!onRegister && (
                  <LinkButton
                    href="/register"
                    variant="secondary"
                    size="sm"
                    className="h-8 rounded-lg px-3 text-sm sm:h-9"
                  >
                    注册
                  </LinkButton>
                )}
              </>
            )}
          </div>
        </nav>
      </div>
    </header>
  );
}
