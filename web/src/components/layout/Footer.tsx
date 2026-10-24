import { Link } from "react-router-dom";

export const Footer = () => (
  <footer className="border-t border-kumo-line bg-kumo-base">
    <div className="shell flex flex-col gap-3 py-7 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
      <span>Emailbox · 第三方邮箱批量托管与 API 服务</span>
      <nav aria-label="页脚导航" className="flex flex-wrap gap-x-5 gap-y-2">
        <Link className="hover:text-kumo-strong" to="/pricing">
          价格与额度
        </Link>
        <Link className="hover:text-kumo-strong" to="/legal/privacy-policy">
          隐私政策
        </Link>
        <Link className="hover:text-kumo-strong" to="/legal/terms">
          服务条款
        </Link>
      </nav>
    </div>
  </footer>
);
