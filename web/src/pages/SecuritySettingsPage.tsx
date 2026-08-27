import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { userApi } from "@/api";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useAuthStore } from "@/store/authStore";
import { SettingsPage } from "./ProfileSettingsPage";

export default function SecuritySettingsPage() {
  const [oldPassword, setOldPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const { error, pending, run } = useAsyncAction();
  const navigate = useNavigate();
  const clearAuth = useAuthStore((state) => state.clearAuth);

  function submit(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      await userApi.changePassword({ old_password: oldPassword, new_password: newPassword });
      clearAuth();
      navigate("/login");
    });
  }

  return (
    <SettingsPage title="账号安全" description="修改密码后，所有登录会话都会失效。">
      <LayerCard render={<form onSubmit={submit} />} className="max-w-2xl p-6">
        <div className="space-y-5">
          <Input
            label="当前密码"
            type="password"
            value={oldPassword}
            onChange={(event) => setOldPassword(event.target.value)}
            required
          />
          <Input
            label="新密码"
            type="password"
            value={newPassword}
            onChange={(event) => setNewPassword(event.target.value)}
            required
          />
          {error && <p className="text-sm text-kumo-danger">{error}</p>}
        </div>
        <div className="mt-6 border-t border-kumo-line pt-5">
          <Button type="submit" variant="secondary" disabled={pending}>
            {pending ? "更新中…" : "更新密码"}
          </Button>
        </div>
      </LayerCard>
    </SettingsPage>
  );
}
