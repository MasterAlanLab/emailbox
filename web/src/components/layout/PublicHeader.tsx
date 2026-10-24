import { LinkButton } from "@cloudflare/kumo/components/button";
import { ArrowRight, Envelope } from "@phosphor-icons/react";
import { Link, useLocation } from "react-router-dom";
import { useAuthStore } from "@/store/authStore";

// 公开页让访客在注册之前就能看到定价。窄屏只收起品牌文字，
// 保留价格、登录与注册入口，避免横向挤出 320px 的屏幕。

export function PublicHeader() {
  const { pathname } = useLocation();
  const authed = useAuthStore((state) => state.isAuthenticated);
  // 登录页上再放一个「登录」按钮是噪音，注册页同理。
  const onLogin = pathname === "/login";
  const onRegister = pathname === "/register";

  return (
    <header className="sticky top-0 z-40 border-b border-kumo-line bg-kumo-base/80 backdrop-blur">
      <div className="shell flex h-14 items-center justify-between">
        <Link
          to="/"
          aria-label="Emailbox 首页"
          className="flex shrink-0 items-center gap-2.5 text-kumo-strong"
        >
          <span className="grid size-7 place-items-center rounded-lg bg-linear-to-br from-ebx-brand-grad-from to-ebx-brand-grad-to text-white">
            <Envelope size={15} weight="fill" />
          </span>
          <span className="hidden text-sm font-semibold tracking-[-0.01em] sm:inline">
            Emailbox
          </span>
        </Link>

        <nav aria-label="公开导航" className="flex items-center gap-1">
          <LinkButton
            href="/pricing"
            variant="secondary"
            aria-current={pathname === "/pricing" ? "page" : undefined}
          >
            价格
          </LinkButton>
          {/* 已经登录的人看到「登录 / 注册」是件很怪的事——点下去只会被守卫
              重定向回工作台。直接给他要去的地方。 */}
          {authed ? (
            <LinkButton href="/mail" variant="secondary">
              进入控制台
              <ArrowRight size={13} />
            </LinkButton>
          ) : (
            <>
              {!onLogin && (
                <LinkButton href="/login" variant="secondary">
                  登录
                </LinkButton>
              )}
              {!onRegister && (
                <LinkButton href="/register" variant="secondary">
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
