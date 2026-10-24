import { Envelope } from "@phosphor-icons/react";
import { Link } from "react-router-dom";
import { SUPPORT_EMAIL } from "@/lib/support";

const footerLinkClassName =
  "rounded-sm text-kumo-subtle transition-colors hover:text-kumo-strong focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-kumo-focus";

export function Footer({ compact = false }: { compact?: boolean }) {
  const contact = (
    <p className="flex min-w-0 flex-wrap gap-x-1">
      <span>客服：</span>
      <a
        className="break-all rounded-sm text-kumo-link hover:underline focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-kumo-focus"
        href={`mailto:${SUPPORT_EMAIL}`}
      >
        {SUPPORT_EMAIL}
      </a>
    </p>
  );

  if (compact) {
    return (
      <footer className="shrink-0 border-t border-kumo-line bg-kumo-base">
        <div className="mx-auto w-full max-w-7xl px-3 py-2 text-xs text-kumo-subtle sm:px-6 lg:px-8">
          {contact}
        </div>
      </footer>
    );
  }

  return (
    <footer className="shrink-0 border-t border-kumo-line bg-kumo-base">
      <div className="shell">
        <div className="grid gap-10 py-10 sm:py-12 md:grid-cols-2 md:gap-16">
          <div className="min-w-0">
            <Link
              to="/"
              aria-label="Emailbox 首页"
              className="inline-flex items-center gap-2.5 rounded-sm text-kumo-strong focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-kumo-focus"
            >
              <span className="grid size-7 place-items-center rounded-lg bg-linear-to-br from-ebx-brand-grad-from to-ebx-brand-grad-to text-white">
                <Envelope size={15} weight="fill" />
              </span>
              <span className="text-lg font-semibold tracking-[-0.02em]">Emailbox</span>
            </Link>
            <p className="mt-4 max-w-64 text-sm leading-6 text-kumo-subtle">
              第三方邮箱集中托管，
              <br />
              统一收信与批量运维。
            </p>
          </div>
          <nav aria-label="页脚导航" className="grid grid-cols-2 gap-x-8 text-sm">
            <div>
              <h2 className="font-medium text-kumo-strong">产品与服务</h2>
              <ul className="mt-4 space-y-3">
                <li>
                  <Link className={footerLinkClassName} to="/pricing">
                    价格与额度
                  </Link>
                </li>
                <li>
                  <Link className={footerLinkClassName} to="/mail">
                    进入控制台
                  </Link>
                </li>
              </ul>
            </div>
            <div>
              <h2 className="font-medium text-kumo-strong">法律与隐私</h2>
              <ul className="mt-4 space-y-3">
                <li>
                  <Link className={footerLinkClassName} to="/legal/privacy-policy">
                    隐私政策
                  </Link>
                </li>
                <li>
                  <Link className={footerLinkClassName} to="/legal/terms">
                    服务条款
                  </Link>
                </li>
              </ul>
            </div>
          </nav>
        </div>
        <div className="flex flex-col gap-3 border-t border-kumo-line py-6 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
          <p>© {new Date().getFullYear()} Emailbox</p>
          {contact}
        </div>
      </div>
    </footer>
  );
}
