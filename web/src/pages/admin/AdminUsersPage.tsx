import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useCallback, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { adminApi, type AdminUser } from "@/api/admin";
import { AdminShell } from "@/components/admin/AdminShell";
import { QuotaDialog, type QuotaTarget } from "@/components/admin/QuotaDialog";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useAuthStore } from "@/store/authStore";

// 后台只有这一份人员清单。
//
// **一个租户空间只属于一个用户**，所以「工作空间列表」和「用户列表」原本是同一批行
// 的两种叫法——两份清单摆在一起，只会让人怀疑「这两个数为什么对不上」。
// 配额调整和「进入其邮箱」因此都并到了这一行的操作里。

const COLUMNS = "grid-cols-[minmax(11rem,2fr)_7rem_5rem_4.5rem_auto]";

export default function AdminUsersPage() {
  const me = useAuthStore((s) => s.user);
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState("");
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState("");
  const [reloadToken, setReloadToken] = useState(0);
  const [quotaTarget, setQuotaTarget] = useState<QuotaTarget | null>(null);
  // 临时密码只在重置那一次的响应里出现，拿到之后必须一直显示到管理员主动关掉，
  // 否则他一刷新页面就再也拿不到了，只能再重置一次。
  const [tempPassword, setTempPassword] = useState<{ username: string; password: string } | null>(
    null,
  );
  const { error, pending, run } = useAsyncAction();

  const reload = useCallback(() => setReloadToken((v) => v + 1), []);

  useEffect(() => {
    let ignore = false;
    void (async () => {
      try {
        const resp = await adminApi.users({ q: query || undefined, status: status || undefined });
        if (ignore) return;
        setUsers(resp.data.items);
        setTotal(resp.data.pagination.total);
        setLoadError("");
      } catch (e) {
        if (!ignore) setLoadError(e instanceof Error ? e.message : "加载失败");
      } finally {
        if (!ignore) setLoading(false);
      }
    })();
    return () => {
      ignore = true;
    };
  }, [query, status, reloadToken]);

  const act = (fn: () => Promise<unknown>) =>
    void run(async () => {
      await fn();
      reload();
    });

  return (
    <AdminShell title="用户" description={`共 ${total} 位注册用户。`}>
      <div className="mb-4 flex flex-wrap items-center gap-3">
        <Input
          className="max-w-xs"
          placeholder="搜索用户名或邮箱"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
        />
        {/* 用 items 而不是 <Select.Option> 子元素：后者在 value="" 时选不中，
            下拉框会显示成一片空白，看起来像坏了。 */}
        <Select
          className="w-36"
          size="sm"
          aria-label="按状态筛选"
          items={[
            { label: "全部状态", value: "" },
            { label: "正常", value: "active" },
            { label: "已停用", value: "disabled" },
          ]}
          value={status}
          onValueChange={(v: string | null) => setStatus(v ?? "")}
        />
      </div>

      {tempPassword && (
        <LayerCard className="mb-4 flex flex-wrap items-center gap-3 border-kumo-line p-4">
          <span className="text-sm">
            已为 <strong>{tempPassword.username}</strong> 生成临时密码：
            <code className="ml-2 rounded bg-kumo-tint px-2 py-1 font-mono">
              {tempPassword.password}
            </code>
          </span>
          <span className="text-xs text-kumo-subtle">
            关闭后无法再次查看，请立即转交给用户并提示其尽快修改。
          </span>
          <Button size="sm" variant="secondary" onClick={() => setTempPassword(null)}>
            我已记录
          </Button>
        </LayerCard>
      )}

      {(error || loadError) && (
        <p className="mb-4 text-sm text-kumo-danger">{error || loadError}</p>
      )}

      <LayerCard className="overflow-x-auto">
        <div
          className={`grid ${COLUMNS} gap-3 border-b border-kumo-line bg-kumo-canvas px-4 py-3 text-xs font-medium text-kumo-subtle`}
        >
          <span>用户</span>
          <span>邮箱 / 上限</span>
          <span>套餐</span>
          <span>状态</span>
          <span className="text-right">操作</span>
        </div>
        <div className="rule-list">
          {loading && <p className="px-4 py-4 text-sm text-kumo-subtle">加载中…</p>}
          {!loading && users.length === 0 && (
            <p className="px-4 py-4 text-sm text-kumo-subtle">没有匹配的用户。</p>
          )}
          {users.map((user) => (
            <div key={user.id} className={`grid ${COLUMNS} items-center gap-3 px-4 py-3 text-sm`}>
              <div className="min-w-0">
                <p className="truncate font-medium text-kumo-strong">
                  {user.username}
                  {user.platform_role === "admin" && (
                    <span className="ml-2 rounded bg-kumo-tint px-1.5 py-0.5 text-xs text-kumo-subtle">
                      管理员
                    </span>
                  )}
                </p>
                {/* 邮箱是可选的，没填就不留一行空白 */}
                {user.email && <p className="truncate text-xs text-kumo-subtle">{user.email}</p>}
              </div>

              <span className={user.over_quota ? "text-kumo-danger" : ""}>
                {user.account_count} / {user.max_accounts < 0 ? "不限" : user.max_accounts}
                {/* 超额是调低配额之后的合法状态（不追溯删除已有数据），标出来但不当成错误 */}
                {user.over_quota && <span className="ml-1 text-xs">超额</span>}
              </span>
              <span className="truncate text-xs text-kumo-subtle">{user.plan_code || "-"}</span>
              <span className={user.status === "active" ? "" : "text-kumo-danger"}>
                {user.status === "active" ? "正常" : "已停用"}
              </span>

              <div className="flex justify-end gap-2">
                {/* 没有租户空间的老账号（000002_saas 之前建的）没有邮箱可看，
                    也没有配额可调，这两个入口对它们直接不渲染。 */}
                {user.tenant_id && (
                  <>
                    <Link
                      to={`/admin/tenants/${user.tenant_id}/mail`}
                      className="inline-flex items-center text-sm text-kumo-link hover:underline"
                    >
                      {/* 不叫「邮箱」：左侧导航栏里已经有一个「邮箱」，
                          同名两处指向完全不同的地方，很容易点错。 */}
                      进入邮箱
                    </Link>
                    <Button
                      size="sm"
                      variant="secondary"
                      onClick={() =>
                        setQuotaTarget({
                          tenantID: user.tenant_id,
                          title: user.username,
                          subtitle: user.email || "未设置邮箱",
                        })
                      }
                    >
                      配额
                    </Button>
                  </>
                )}
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={pending || user.id === me?.id}
                  onClick={() =>
                    act(() =>
                      adminApi.updateUser(user.id, {
                        status: user.status === "active" ? "disabled" : "active",
                      }),
                    )
                  }
                >
                  {user.status === "active" ? "停用" : "启用"}
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={pending}
                  onClick={() =>
                    act(() =>
                      adminApi.updateUser(user.id, {
                        platform_role: user.platform_role === "admin" ? "user" : "admin",
                      }),
                    )
                  }
                >
                  {user.platform_role === "admin" ? "撤销管理员" : "设为管理员"}
                </Button>
                <Button
                  size="sm"
                  variant="secondary"
                  disabled={pending}
                  onClick={() =>
                    void run(async () => {
                      const resp = await adminApi.resetPassword(user.id);
                      setTempPassword({ username: user.username, password: resp.data.password });
                      reload();
                    })
                  }
                >
                  重置密码
                </Button>
                <Button
                  size="sm"
                  variant="secondary-destructive"
                  disabled={pending || user.id === me?.id}
                  onClick={() => {
                    // 删除会连带清掉这个人所有邮箱的凭据，且不可撤销，必须二次确认。
                    if (
                      !window.confirm(
                        `删除 ${user.username}？其 ${user.account_count} 个邮箱账号会一并删除，凭据将被物理清除，无法恢复。`,
                      )
                    ) {
                      return;
                    }
                    act(() => adminApi.deleteUser(user.id));
                  }}
                >
                  删除
                </Button>
              </div>
            </div>
          ))}
        </div>
      </LayerCard>

      {quotaTarget && (
        <QuotaDialog
          target={quotaTarget}
          onClose={() => setQuotaTarget(null)}
          onSaved={() => {
            setQuotaTarget(null);
            reload();
          }}
        />
      )}
    </AdminShell>
  );
}
