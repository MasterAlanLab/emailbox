import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { X } from "@phosphor-icons/react";
import { useState } from "react";
import { mailApi, type MailAccount, type MailGroupNode, type TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";

interface AccountDrawerProps {
  tenantID: TenantRef;
  account: MailAccount;
  groups: MailGroupNode[];
  onClose: () => void;
  onSaved: () => void;
}

export function AccountDrawer({ tenantID, account, groups, onClose, onSaved }: AccountDrawerProps) {
  const [remark, setRemark] = useState(account.remark);
  const [groupID, setGroupID] = useState(account.group_id);
  const [aliases, setAliases] = useState(account.aliases.join("\n"));
  // 凭据留空表示保持原值——后端把 nil 当「不修改」，
  // 因此这里绝不能把空串当成用户想清空。
  const [password, setPassword] = useState("");
  const [refreshToken, setRefreshToken] = useState("");
  const { error, pending, run } = useAsyncAction();

  function save(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      const payload: Record<string, unknown> = {
        remark,
        group_id: groupID,
        aliases: aliases
          .split("\n")
          .map((a) => a.trim())
          .filter(Boolean),
      };
      // 只有真的填了才发送，避免把「没改」变成「清空」。
      if (password) payload.password = password;
      if (refreshToken) payload.refresh_token = refreshToken;
      await mailApi.updateAccount(tenantID, account.id, payload);
      onSaved();
      onClose();
    });
  }

  return (
    <aside className="flex w-96 shrink-0 flex-col border-l border-kumo-line bg-kumo-elevated">
      <header className="flex items-start justify-between gap-2 border-b border-kumo-line px-4 py-3">
        <div className="min-w-0">
          <h2 className="truncate font-medium">{account.email}</h2>
          <p className="mt-0.5 text-xs text-kumo-subtle">
            {account.provider} · {account.account_type}
          </p>
        </div>
        <Button size="sm" variant="ghost" icon={X} onClick={onClose} aria-label="关闭" />
      </header>

      <form onSubmit={save} className="flex-1 space-y-5 overflow-y-auto p-4">
        <section className="space-y-2 text-sm">
          <Row label="状态">
            <Badge variant={account.status === "active" ? "green" : "neutral"}>
              {account.status}
            </Badge>
          </Row>
          <Row label="最近刷新">
            <span className={account.last_refresh_status === "failed" ? "text-kumo-danger" : ""}>
              {account.last_refresh_status === "never" ? "从未刷新" : account.last_refresh_status}
            </span>
          </Row>
          {account.last_refresh_error && (
            <p className="rounded bg-kumo-recessed p-2 text-xs text-kumo-danger">
              {account.last_refresh_error}
            </p>
          )}
          <Row label="通道">{account.auth_channel || "未记录"}</Row>
          {account.proxy_url_masked && <Row label="代理">{account.proxy_url_masked}</Row>}
        </section>

        <label className="block text-sm font-medium">
          分组
          <select
            className="mt-2 block min-h-9 w-full rounded-md border border-kumo-line bg-kumo-base px-2 text-sm font-normal"
            value={groupID}
            onChange={(event) => setGroupID(event.target.value)}
          >
            {groups.map((group) => (
              <option key={group.id} value={group.id}>
                {group.name}
              </option>
            ))}
          </select>
        </label>

        <Input label="备注" value={remark} onChange={(event) => setRemark(event.target.value)} />

        <label className="block text-sm font-medium">
          别名（每行一个）
          <textarea
            className="mt-2 block w-full rounded-md border border-kumo-line bg-kumo-base p-2 font-mono text-xs font-normal"
            rows={3}
            value={aliases}
            onChange={(event) => setAliases(event.target.value)}
          />
        </label>

        <fieldset className="space-y-3 rounded-md border border-kumo-line p-3">
          <legend className="px-1 text-xs text-kumo-subtle">凭据（留空表示不修改）</legend>
          <Input
            label="密码"
            type="password"
            placeholder={account.has_password ? "已设置" : "未设置"}
            value={password}
            onChange={(event) => setPassword(event.target.value)}
          />
          <Input
            label="refresh_token"
            type="password"
            placeholder={account.has_refresh_token ? "已设置" : "未设置"}
            value={refreshToken}
            onChange={(event) => setRefreshToken(event.target.value)}
          />
        </fieldset>

        {error && <p className="text-sm text-kumo-danger">{error}</p>}
        <Button type="submit" variant="secondary" className="w-full" disabled={pending}>
          {pending ? "保存中…" : "保存"}
        </Button>
      </form>
    </aside>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex items-start justify-between gap-3">
      <span className="shrink-0 text-kumo-subtle">{label}</span>
      <span className="min-w-0 truncate text-right">{children}</span>
    </div>
  );
}
