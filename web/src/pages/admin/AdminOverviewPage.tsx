import { useEffect, useState } from "react";
import { adminApi, type PlatformStats } from "@/api/admin";
import { AdminShell, StatTile } from "@/components/admin/AdminShell";

export default function AdminOverviewPage() {
  const [stats, setStats] = useState<PlatformStats | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    let ignore = false;
    void (async () => {
      try {
        const resp = await adminApi.stats();
        if (!ignore) setStats(resp.data);
      } catch (e) {
        if (!ignore) setError(e instanceof Error ? e.message : "加载失败");
      }
    })();
    return () => {
      ignore = true;
    };
  }, []);

  return (
    <AdminShell title="平台总览" description="全系统的用户与邮箱概况。">
      {error && <p className="text-sm text-kumo-danger">{error}</p>}
      {!stats && !error && <p className="text-sm text-kumo-subtle">加载中…</p>}

      {stats && (
        <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
          <StatTile
            label="用户"
            value={stats.user_count}
            hint={`其中 ${stats.admin_count} 位管理员`}
          />
          <StatTile
            label="已禁用用户"
            value={stats.disabled_user_count}
            alert={stats.disabled_user_count > 0}
          />
          <StatTile label="邮箱账号" value={stats.account_count} />
          {/* 被封账号单独列出来：它是协议层识别到服务商封禁后置位的，
              数量突然上涨通常意味着某批账号的来源出了问题，值得当天就发现。 */}
          <StatTile
            label="已封禁邮箱"
            value={stats.banned_account_count}
            alert={stats.banned_account_count > 0}
          />
          <StatTile label="今日拉信" value={stats.mail_fetch_today} />
          <StatTile label="今日刷新令牌" value={stats.token_refresh_today} />
        </div>
      )}
    </AdminShell>
  );
}
