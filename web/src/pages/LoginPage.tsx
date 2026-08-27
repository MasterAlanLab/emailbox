import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useAuthStore } from "@/store/authStore";
import { useTenantStore } from "@/store/tenantStore";

export default function LoginPage() {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const { error, pending, run } = useAsyncAction();
  const login = useAuthStore((state) => state.login);
  const hydrate = useTenantStore((state) => state.hydrate);
  const navigate = useNavigate();

  function submit(event: React.FormEvent) {
    event.preventDefault();
    // 登录响应本身就带着租户列表，直接用它填充，无需再请求一次 /tenants。
    void run(async () => {
      hydrate(await login({ username, password }));
      navigate("/mail");
    });
  }

  return (
    <AuthCard title="欢迎回来" description="用你的用户名和密码继续。">
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
          autoComplete="current-password"
          value={password}
          onChange={(event) => setPassword(event.target.value)}
          required
        />
        {error && <p className="text-sm text-kumo-danger">{error}</p>}
        <SubmitButton pending={pending} label="登录" pendingLabel="登录中…" />
        <p className="pt-2 text-center text-sm text-kumo-subtle">
          还没有账号？{" "}
          <Link className="font-medium text-kumo-link hover:underline" to="/register">
            创建账号
          </Link>
        </p>
      </form>
    </AuthCard>
  );
}

// 登录卡片里的主按钮：宽度跟着文字走，在卡片里居中。
//
// 此前是 w-full——整块 352px 宽的实心按钮，和上面的输入框一样宽，
// 视觉重量压过了整张卡片。表单里的输入框需要满宽（用户要往里打字），
// 按钮不需要：它的可点区域只要够手指点，再宽就只是块色斑。
//
// 两个页面共用，是为了让「登录」和「创建账号」在同一个位置、同一种形状——
// 各写各的迟早会分叉成一个居中一个满宽。
export function SubmitButton({
  pending,
  label,
  pendingLabel,
}: {
  pending: boolean;
  label: string;
  pendingLabel: string;
}) {
  return (
    <div className="mt-1 flex justify-center">
      {/* px-10 而不是固定宽度：中文按钮文案长短差得多（「登录」两字 vs
          「创建账号」四字），钉死宽度会让短的那个显得空。 */}
      <Button type="submit" variant="primary" size="lg" className="px-10" disabled={pending}>
        {pending ? pendingLabel : label}
      </Button>
    </div>
  );
}

export function AuthCard({
  title,
  description,
  children,
}: {
  title: string;
  description?: string;
  children: React.ReactNode;
}) {
  return (
    <div className="grid flex-1 place-items-center bg-kumo-canvas px-5 py-16">
      <div className="w-full max-w-[400px]">
        <div className="mb-8 text-center">
          <h1 className="display-md text-kumo-strong">{title}</h1>
          {description && <p className="mt-2 text-sm text-kumo-subtle">{description}</p>}
        </div>
        <div className="rounded-lg border border-kumo-line bg-kumo-elevated p-6">{children}</div>
      </div>
    </div>
  );
}
