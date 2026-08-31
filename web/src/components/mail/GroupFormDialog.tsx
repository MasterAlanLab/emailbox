import { Button } from "@cloudflare/kumo/components/button";
import { Input } from "@cloudflare/kumo/components/input";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useEffect, useState } from "react";
import { asScope, mailApi, type MailGroupNode, type TenantRef } from "@/api/mail";
import { useAsyncAction } from "@/lib/useAsyncAction";

interface GroupFormDialogProps {
  tenantID: TenantRef;
  /** 传了就是编辑，不传就是新建。 */
  group?: MailGroupNode;
  onClose: () => void;
  onSaved: () => void;
}

// 代理明文的取数状态。新建时无需取数，直接就是 ready。
type ProxyLoad = "ready" | "loading" | "failed";

export function GroupFormDialog({ tenantID, group, onClose, onSaved }: GroupFormDialogProps) {
  const editing = group !== undefined;
  const [name, setName] = useState(group?.name ?? "");
  const [description, setDescription] = useState(group?.description ?? "");
  const [proxy, setProxy] = useState("");
  const [proxyLoad, setProxyLoad] = useState<ProxyLoad>(editing ? "loading" : "ready");
  const { error, pending, run } = useAsyncAction();

  // scope 对象每次渲染都是新的，直接进依赖数组会让 effect 每帧重跑一次
  // ——那是三次带审计的凭据读取。拆成原始值再在 effect 里拼回去（同 MailSidebar 取配额那处）。
  const scope = asScope(tenantID);
  const scopeTenantID = scope.tenantID;
  const scopeAdmin = scope.admin ?? false;
  const groupID = group?.id;

  // 编辑时把代理明文取回来填进输入框。
  //
  // 不能拿列表里的打码串回填：用户进来只改个名字，一按保存
  // "socks5://u:****@host:1080" 就被当成口令写回库里，代理从此是坏的，
  // 而界面上一切正常——直到某个账号取信失败才会发现。
  //
  // 明文单独走 GET /groups/:id/proxy：那个端点要 account:secret 权限，
  // 且每次调用都写一条审计。所以只在打开编辑框时取这一次。
  useEffect(() => {
    if (!groupID) return undefined;
    let ignore = false;
    void mailApi
      .groupProxy({ tenantID: scopeTenantID, admin: scopeAdmin }, groupID)
      .then((r) => {
        if (ignore) return;
        setProxy(r.data.proxy_url);
        setProxyLoad("ready");
      })
      .catch(() => {
        if (!ignore) setProxyLoad("failed");
      });
    return () => {
      ignore = true;
    };
  }, [groupID, scopeTenantID, scopeAdmin]);

  function submit(event: React.FormEvent) {
    event.preventDefault();
    void run(async () => {
      // 明文还没到手（loading）或没取到（failed）时省掉代理字段：
      // PATCH 的语义是「不传就保持原值」，而这个输入框此刻还是空的，
      // 照发等于把用户配好的代理静默清掉。ready 时才发，此时空串就是真的要清空。
      const proxyFields = proxyLoad === "ready" ? { proxy_url: proxy.trim() } : {};
      if (editing) {
        await mailApi.updateGroup(tenantID, group.id, { name, description, ...proxyFields });
      } else {
        await mailApi.createGroup(tenantID, { name, description, ...proxyFields });
      }
      onSaved();
    });
  }

  const proxyDisabled = proxyLoad !== "ready";

  return (
    <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <LayerCard
        render={<form onSubmit={submit} />}
        className="max-h-full w-full max-w-md overflow-auto p-5"
      >
        <h2 className="text-lg font-semibold text-kumo-strong">
          {editing ? "编辑分组" : "新建分组"}
        </h2>

        <div className="mt-4 flex flex-col gap-3">
          <label className="flex flex-col gap-1 text-sm">
            名称
            {/* 外面这层 <label> 包住输入框已经足够把两者关联起来，但 Kumo 的 Input
                自己查的是 label / aria-label / aria-labelledby 三个 prop，查不到就
                往控制台打警告。给一个 aria-label 把它满足掉，视觉上没有变化。 */}
            <Input
              aria-label="名称"
              value={name}
              onChange={(e) => setName(e.target.value)}
              maxLength={100}
              autoFocus
              required
            />
          </label>

          <label className="flex flex-col gap-1 text-sm">
            描述（可选）
            <Input
              aria-label="描述"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              placeholder="例如：这批账号只用于 A 客户的验证码接收"
            />
          </label>
        </div>

        <div className="mt-5">
          <div className="flex items-baseline justify-between gap-2">
            <h3 className="text-sm font-medium text-kumo-strong">代理</h3>
            {proxyLoad === "loading" && <span className="text-xs text-kumo-subtle">读取中…</span>}
          </div>

          {proxyLoad === "failed" && (
            <p className="mt-2 text-sm text-kumo-danger">
              代理读取失败。可以照常改名称，本次保存不会改动代理配置。
            </p>
          )}

          {/* 上面的 <h3> 标题已经是「代理」，这里不再重复展示一遍可见标签，
              aria-label 留着满足 Kumo Input 的无障碍要求。
              分组代理是兜底不是覆盖：账号自己填了代理就整组用账号那份
              （mailer.ResolveProxy）。地址按要求明文显示，含口令。autoComplete 关掉：
              这串里带口令，让浏览器把它当密码存进去，等于在另一个地方多留了一份明文。 */}
          <Input
            className="mt-3 w-full"
            aria-label="代理"
            value={proxy}
            onChange={(e) => setProxy(e.target.value)}
            disabled={proxyDisabled}
            placeholder="socks5://user:pass@host:1080"
            autoComplete="off"
            spellCheck={false}
          />

          <p className="mt-2 text-xs text-kumo-subtle">
            支持 socks5://、socks5h://、http://。注意：http:// 仅对 Graph 通道生效。
          </p>
        </div>

        {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}

        <div className="mt-5 flex justify-end gap-2">
          <Button type="button" variant="secondary" onClick={onClose}>
            取消
          </Button>
          <Button type="submit" variant="secondary" disabled={pending || !name.trim()}>
            {pending ? "保存中…" : editing ? "保存" : "创建"}
          </Button>
        </div>
      </LayerCard>
    </div>
  );
}
