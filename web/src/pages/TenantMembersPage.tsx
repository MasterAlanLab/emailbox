// ⚠ 这个页面当前**没有挂在路由上**，界面上没有入口。
//
// 产品现在的形态是「一个租户空间只属于一个用户」，不展示工作区概念（10 文档 §3）。
// 文件留着是因为多租户的数据模型和 API 都还在，等要做团队协作时把它挂回
// src/router/index.tsx 即可——所以这里的「工作区」措辞是对的，别顺手改成「用户」。

import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useEffect, useState } from "react";
import { tenantApi, type TenantRole } from "@/api";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useTenantStore } from "@/store/tenantStore";
import { SettingsPage } from "./ProfileSettingsPage";

export default function TenantMembersPage() {
  const { activeTenant, members, membership, loadMembers } = useTenantStore();
  const [username, setUsername] = useState("");
  const [role, setRole] = useState<TenantRole>("member");
  const [loadError, setLoadError] = useState("");
  const [loadedFor, setLoadedFor] = useState<string | null>(null);
  const { error, pending, run } = useAsyncAction();
  const activeTenantID = activeTenant?.id ?? null;
  // 依赖 id 而非对象：loadTenants 每次都会产出新对象，依赖对象会让本效果重复触发。
  useEffect(() => {
    let cancelled = false;
    loadMembers()
      .then(() => !cancelled && setLoadError(""))
      .catch((caught: Error) => !cancelled && setLoadError(caught.message))
      .finally(() => !cancelled && setLoadedFor(activeTenantID));
    // 切换工作区时丢弃上一次请求的结果，避免先发后到覆盖新数据。
    return () => {
      cancelled = true;
    };
  }, [activeTenantID, loadMembers]);
  // 由「已为当前工作区加载完成」推导，避免在效果里同步 setState。
  const loaded = loadedFor === activeTenantID;
  // 成员列表未加载完时不能认为有管理权限，否则会短暂显示越权的管理表单。
  const canManage = loaded && (membership?.role === "owner" || membership?.role === "admin");

  function add(event: React.FormEvent) {
    event.preventDefault();
    if (!activeTenant) return;
    void run(async () => {
      await tenantApi.addMember(activeTenant.id, { username, role });
      setUsername("");
      setRole("member");
      await loadMembers();
    });
  }

  function remove(userID: string) {
    if (!activeTenant) return;
    void run(async () => {
      await tenantApi.removeMember(activeTenant.id, userID);
      await loadMembers();
    });
  }

  return (
    <SettingsPage title="成员" description="查看工作区成员并管理他们的访问级别。">
      {canManage && (
        <LayerCard
          render={<form onSubmit={add} />}
          className="mb-6 flex flex-col gap-3 p-4 sm:flex-row"
        >
          <Input
            className="flex-1"
            aria-label="用户名"
            placeholder="已注册用户的用户名"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            required
          />
          <Select
            aria-label="角色"
            className="sm:w-36"
            value={role}
            onValueChange={(value: TenantRole | null) => value && setRole(value)}
          >
            <Select.Option value="member">Member</Select.Option>
            <Select.Option value="admin">Admin</Select.Option>
            {/* 只有 owner 能授予 owner：后端也会拒，这里不显示是为了别给出做不到的选项 */}
            {membership?.role === "owner" && <Select.Option value="owner">Owner</Select.Option>}
          </Select>
          <Button type="submit" variant="secondary" disabled={pending}>
            {pending ? "处理中…" : "添加成员"}
          </Button>
        </LayerCard>
      )}
      {(error || loadError) && (
        <p className="mb-6 text-sm text-kumo-danger">{error || loadError}</p>
      )}
      <LayerCard>
        <div className="grid grid-cols-[1fr_auto] border-b border-kumo-line bg-kumo-canvas px-5 py-3 text-xs font-medium text-kumo-subtle">
          <span>用户</span>
          <span>角色</span>
        </div>
        <div className="rule-list">
          {/* 加载中和「确实没有成员」必须区分开，否则两者看起来完全一样 */}
          {!loaded && <p className="px-5 py-4 text-sm text-kumo-subtle">加载中…</p>}
          {loaded && !loadError && members.length === 0 && (
            <p className="px-5 py-4 text-sm text-kumo-subtle">暂无成员</p>
          )}
          {members.map((member) => (
            <div key={member.id} className="flex items-center justify-between gap-4 px-5 py-4">
              <div className="min-w-0">
                <p className="truncate text-sm font-medium text-kumo-strong">{member.username}</p>
                {/* 邮箱是可选的，没填就不占一行空白 */}
                {member.email && (
                  <p className="mt-0.5 truncate text-sm text-kumo-subtle">{member.email}</p>
                )}
              </div>
              <div className="flex items-center gap-4">
                <span className="text-xs font-medium capitalize text-kumo-subtle">
                  {member.role}
                </span>
                {canManage && member.user_id !== membership?.user_id && (
                  <Button
                    variant="secondary-destructive"
                    size="sm"
                    disabled={pending}
                    onClick={() => remove(member.user_id)}
                  >
                    移除
                  </Button>
                )}
              </div>
            </div>
          ))}
        </div>
      </LayerCard>
    </SettingsPage>
  );
}
