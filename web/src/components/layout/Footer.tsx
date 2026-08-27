import { Link } from "react-router-dom";

export const Footer = () => (
  <footer className="border-t border-kumo-line bg-kumo-base">
    <div className="shell flex flex-col gap-3 py-7 text-xs text-kumo-subtle sm:flex-row sm:items-center sm:justify-between">
      <span>Emailbox · 批量邮箱托管平台</span>
      <div className="flex gap-5">
        <Link className="hover:text-kumo-strong" to="/legal/privacy-policy">
          隐私政策
        </Link>
        <Link className="hover:text-kumo-strong" to="/legal/terms">
          服务条款
        </Link>
      </div>
    </div>
  </footer>
);
