import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useAuthStore } from "@/store/authStore";
import { useTenantStore } from "@/store/tenantStore";
import { AuthCard, SubmitButton } from "./LoginPage";

export default function RegisterPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { error, pending, run } = useAsyncAction();
  const register = useAuthStore((state) => state.register);
  const hydrate = useTenantStore((state) => state.hydrate);
  const navigate = useNavigate();

  function submit(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      hydrate(await register({ username, password }));
      navigate("/mail");
    });
  }

  return (
    <AuthCard title="创建账号" description="只要用户名和密码，邮箱以后想填再填。">
      <form onSubmit={submit} className="space-y-4">
        <Input
          label="用户名"
          autoComplete="username"
          value={username}
          onChange={(event) => setUsername(event.target.value)}
          required
        />
        <Input
          label="密码"
          type="password"
          autoComplete="new-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          required
        />
        {error && <p className="text-sm text-kumo-danger">{error}</p>}
        <SubmitButton pending={pending} label="创建账号" pendingLabel="创建中…" />
        {/* 07 文档 §3 合规提醒：注册页与导入页各展示一次授权提示。
            托管的是第三方邮箱凭据，这句话必须在用户导入任何东西之前先出现一次。 */}
        <p className="text-center text-xs leading-5 text-kumo-subtle">
          注册即表示你同意
          <Link className="text-kumo-link hover:underline" to="/legal/terms">
            服务条款
          </Link>
          ，并确认只会托管你拥有合法授权的邮箱账号。
        </p>
        <p className="pt-2 text-center text-sm text-kumo-subtle">
          已有账号？{" "}
          <Link className="font-medium text-kumo-strong hover:underline" to="/login">
            登录
          </Link>
        </p>
      </form>
    </AuthCard>
  );
}
