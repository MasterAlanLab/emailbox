import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useState } from "react";
import { PageShell } from "@/components/layout/PageShell";
import { userApi } from "@/api";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useAuthStore } from "@/store/authStore";

export default function ProfileSettingsPage() {
  const user = useAuthStore((state) => state.user)!;
  const setUser = useAuthStore((state) => state.setUser);
  const [username, setUsername] = useState(user.username);
  // 后端把 email 当指针处理：发空串是「清空」，不发才是「保持原值」。
  // 这个表单一次提交全部字段，所以清空邮箱只要把输入框留白即可。
  const [email, setEmail] = useState(user.email);
  const [saved, setSaved] = useState(false);
  const { error, pending, run } = useAsyncAction();

  function submit(event: React.FormEvent) {
    event.preventDefault();
    setSaved(false);
    void run(async () => {
      const response = await userApi.updateProfile({ username, email });
      setUser(response.data);
      setSaved(true);
    });
  }

  return (
    <SettingsPage title="个人资料" description="用户名用于登录，邮箱可填可不填。">
      <LayerCard render={<form onSubmit={submit} />} className="max-w-2xl p-6">
        <div className="space-y-5">
          <Input
            label="用户名"
            value={username}
            onChange={(event) => setUsername(event.target.value)}
            required
          />
          <Input
            label="邮箱（可选）"
            type="email"
            value={email}
            onChange={(event) => setEmail(event.target.value)}
          />
          {/* 邮箱目前不参与任何流程：不用来登录、不发通知、不做找回。
              说清楚这一点，比让用户猜「不填会不会出事」强。 */}
          <p className="-mt-2 text-xs text-kumo-subtle">不用于登录，也不会收到邮件。</p>
          {error && <p className="text-sm text-kumo-danger">{error}</p>}
        </div>
        <div className="mt-6 flex items-center gap-3 border-t border-kumo-line pt-5">
          <Button type="submit" variant="primary" disabled={pending}>
            {pending ? "保存中…" : "保存更改"}
          </Button>
          {saved && <span className="text-sm text-kumo-subtle">已保存</span>}
        </div>
      </LayerCard>
    </SettingsPage>
  );
}

export function SettingsPage({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  // 排版与间距交给 PageShell，这里只管「设置有哪几个页签」。
  // 表单在 children 里自己限宽，外层保持和其他页面同一条起始线。
  return (
    <PageShell title={title} description={description} tabs={SETTINGS_TABS}>
      {children}
    </PageShell>
  );
}

// 「用量」不在这里：它是左侧导航栏的一级入口，不属于「个人资料」这一组。
// 同一个页面从两个层级都能进，用户会以为是两个不同的东西。
const SETTINGS_TABS = [
  { to: "/settings/profile", label: "资料" },
  { to: "/settings/security", label: "安全" },
];
