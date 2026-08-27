// ⚠ 这个页面当前**没有挂在路由上**，界面上没有入口。
//
// 产品现在的形态是「一个租户空间只属于一个用户」，不展示工作区概念（10 文档 §3）。
// 文件留着是因为多租户的数据模型和 API 都还在，等要做团队协作时把它挂回
// src/router/index.tsx 即可——所以这里的「工作区」措辞是对的，别顺手改成「用户」。

import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useState } from "react";
import { tenantApi, type Tenant } from "@/api";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useTenantStore } from "@/store/tenantStore";
import { SettingsPage } from "./ProfileSettingsPage";

export default function TenantSettingsPage() {
  const { activeTenant, loadTenants } = useTenantStore();
  const [newName, setNewName] = useState("");
  const { error, pending, run } = useAsyncAction();

  function create(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      await tenantApi.create({ name: newName, slug: "" });
      setNewName("");
      await loadTenants();
    });
  }

  return (
    <SettingsPage title="工作区设置" description="管理当前工作区，或创建一个新的工作区。">
      <div className="grid gap-6 lg:grid-cols-2">
        {activeTenant && (
          <CurrentTenantForm key={activeTenant.id} tenant={activeTenant} reload={loadTenants} />
        )}
        <LayerCard render={<form onSubmit={create} />} className="p-6">
          <h2 className="text-lg font-medium">新建工作区</h2>
          <p className="mt-1 text-sm text-kumo-subtle">为另一个团队或项目创建独立空间。</p>
          <div className="mt-6">
            <Input
              label="名称"
              value={newName}
              onChange={(event) => setNewName(event.target.value)}
              required
            />
            {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}
          </div>
          <div className="mt-6 border-t border-kumo-line pt-5">
            <Button type="submit" variant="primary" disabled={pending}>
              {pending ? "创建中…" : "创建工作区"}
            </Button>
          </div>
        </LayerCard>
      </div>
    </SettingsPage>
  );
}

function CurrentTenantForm({ tenant, reload }: { tenant: Tenant; reload: () => Promise<void> }) {
  const [name, setName] = useState(tenant.name);
  const [slug, setSlug] = useState(tenant.slug);
  const { error, pending, run } = useAsyncAction();

  function update(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      await tenantApi.update(tenant.id, { name, slug });
      await reload();
    });
  }

  return (
    <LayerCard render={<form onSubmit={update} />} className="p-6">
      <h2 className="text-lg font-medium">当前工作区</h2>
      <p className="mt-1 text-sm text-kumo-subtle">更新名称和 URL 标识。</p>
      <div className="mt-6 space-y-5">
        <Input
          label="名称"
          value={name}
          onChange={(event) => setName(event.target.value)}
          required
        />
        <Input
          label="Slug"
          value={slug}
          onChange={(event) => setSlug(event.target.value)}
          required
        />
        {error && <p className="text-sm text-kumo-danger">{error}</p>}
      </div>
      <div className="mt-6 border-t border-kumo-line pt-5">
        <Button type="submit" variant="primary" disabled={pending}>
          {pending ? "保存中…" : "保存更改"}
        </Button>
      </div>
    </LayerCard>
  );
}
