import { Warning } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { adminApi, type AdminUser } from "@/api/admin";
import MailPage from "@/pages/mail/MailPage";

// 管理员视角的邮箱工作台。
//
// 页面本体**完全复用** /mail 的组件树，只把 scope 换成管理员前缀
// （05 文档 §12.2 要求两侧接口同构，正是为了让这里能一行接上）。
// 这里额外做的只有一件事：常驻提示「你正在看的不是自己的邮箱」。
export default function AdminTenantMailPage() {
  const { tenantID = "" } = useParams();
  const [owner, setOwner] = useState<AdminUser | null>(null);

  useEffect(() => {
    let ignore = false;
    void (async () => {
      try {
        // 一个租户空间只属于一个用户，所以这里找的是「这个空间是谁的」。
        // 没有按 tenantID 查用户的接口，用列表兜一下即可——只是为了在提示里显示名字。
        const resp = await adminApi.users({});
        if (!ignore) setOwner(resp.data.items.find((u) => u.tenant_id === tenantID) ?? null);
      } catch {
        // 名字取不到不影响功能，Banner 退回只显示 ID
      }
    })();
    return () => {
      ignore = true;
    };
  }, [tenantID]);

  return (
    // flex-1 + min-h-0：这个页面也是应用外壳形态（路由 handle.shell），
    // 高度由外壳给定，Banner 占固定的一条，剩下全归 MailPage。
    <div className="flex min-h-0 flex-1 flex-col">
      {/* 这条 Banner 是常驻的，不可关闭：管理员在别人的邮箱里操作时，
          任何一刻都不该以为自己在看自己的数据。
          它由路由决定——跨租户的邮箱只能从这个页面进，所以挂在这里就够；
          将来若有别的入口也能读他人数据，Banner 要跟着一起加。 */}
      <div className="flex shrink-0 flex-wrap items-center gap-3 border-b border-kumo-line bg-kumo-tint px-4 py-2 text-sm">
        <Warning className="text-kumo-danger" size={16} />
        <span>
          你正在以<strong>管理员身份</strong>操作
          {owner ? `「${owner.username}」（${owner.email}）` : ` ${tenantID}`} 的邮箱。
        </span>
        <span className="text-xs text-kumo-subtle">此处的查看与操作都会记入审计日志。</span>
        <Link to="/admin/users" className="ml-auto text-kumo-link hover:underline">
          返回用户列表
        </Link>
      </div>

      <MailPage scope={{ tenantID, admin: true }} />
    </div>
  );
}
