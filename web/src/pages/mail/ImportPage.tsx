import { Button } from "@cloudflare/kumo/components/button";
import { Textarea } from "@cloudflare/kumo/components/input";
import { useEffect, useState } from "react";
import { mailApi, type ImportResult, type MailGroupNode } from "@/api/mail";
import { PageShell } from "@/components/layout/PageShell";
import { findSystemGroup } from "@/components/mail/groupOptions";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useTenantStore } from "@/store/tenantStore";

const FORMATS = [
  { value: "auto", label: "自动识别", hint: "按段数与字段形态判断，混合内容也能处理" },
  {
    value: "outlook_oauth",
    label: "Outlook OAuth（4 段）",
    hint: "邮箱----密码----client_id----refresh_token",
  },
  { value: "imap", label: "标准 IMAP（2 段）", hint: "邮箱----授权码，服务商按域名推断" },
  {
    value: "custom_imap",
    label: "自定义 IMAP（4 段）",
    hint: "邮箱----密码----imap 服务器----端口",
  },
];

export default function ImportPage() {
  const tenantID = useTenantStore((s) => s.activeTenant?.id) ?? "";
  const [groups, setGroups] = useState<MailGroupNode[]>([]);
  // 空串表示「还没拿到分组列表」。真到了提交那一刻它仍是空串也无妨——
  // 后端 resolveGroup 会把空 group_id 落到系统分组，与下拉里预选的那一项同义。
  const [groupID, setGroupID] = useState("");
  const [format, setFormat] = useState("auto");
  const [onConflict, setOnConflict] = useState("skip");
  const [content, setContent] = useState("");
  const [result, setResult] = useState<ImportResult | null>(null);
  const { error, pending, run } = useAsyncAction();

  useEffect(() => {
    if (!tenantID) return undefined;
    let ignore = false;
    void mailApi.groups(tenantID).then((r) => {
      if (ignore) return;
      setGroups(r.data);
      // 预选系统分组，而不是再摆一个硬编码的「默认分组」占位项——
      // 系统分组的名字就叫「默认分组」，两者并列时下拉里会出现两个同名选项，
      // 而它们其实指向同一个分组（一个传空串、一个传 UUID，后端结果一样）。
      setGroupID((prev) => prev || (findSystemGroup(r.data)?.id ?? ""));
    });
    return () => {
      ignore = true;
    };
  }, [tenantID]);

  const lineCount = content.split("\n").filter((l) => l.trim() !== "").length;

  function submit(event: React.FormEvent) {
    event.preventDefault();
    setResult(null);
    void run(async () => {
      const r = await mailApi.importAccounts(tenantID, {
        group_id: groupID,
        format,
        content,
        on_conflict: onConflict,
      });
      setResult(r.data);
    });
  }

  return (
    <PageShell title="批量导入邮箱" description="每行一个账号，字段用 ---- 分隔。">
      {/* 服务条款要求在导入页展示一次授权提示 */}
      <div className="mb-6 rounded-lg border border-kumo-line bg-kumo-warning-tint p-4 text-sm">
        导入即表示你确认对这些邮箱账号拥有合法授权。凭据会加密存储，但请勿导入不属于你的账号。
      </div>

      <form onSubmit={submit} className="grid gap-6 lg:grid-cols-[1fr_320px]">
        <div className="space-y-4">
          <Textarea
            label={`账号内容${lineCount > 0 ? `（${lineCount} 行）` : ""}`}
            rows={16}
            className="font-mono text-xs"
            placeholder={"user@outlook.com----password----client_id----refresh_token"}
            value={content}
            onChange={(event) => setContent(event.target.value)}
            required
          />
          {error && <p className="text-sm text-kumo-danger">{error}</p>}
          <Button type="submit" variant="secondary" size="lg" disabled={pending || !content.trim()}>
            {pending ? "导入中…" : `导入 ${lineCount} 行`}
          </Button>
        </div>

        <aside className="space-y-4">
          <Field label="导入格式">
            <select
              className="min-h-9 w-full rounded-md border border-kumo-line bg-kumo-base px-2 text-sm"
              value={format}
              onChange={(event) => setFormat(event.target.value)}
            >
              {FORMATS.map((f) => (
                <option key={f.value} value={f.value}>
                  {f.label}
                </option>
              ))}
            </select>
            <p className="mt-1 text-xs text-kumo-subtle">
              {FORMATS.find((f) => f.value === format)?.hint}
            </p>
          </Field>

          <Field label="导入到分组">
            <select
              className="min-h-9 w-full rounded-md border border-kumo-line bg-kumo-base px-2 text-sm"
              value={groupID}
              onChange={(event) => setGroupID(event.target.value)}
            >
              {groups.length === 0 && <option value="">加载中…</option>}
              {groups.map((group) => (
                <option key={group.id} value={group.id}>
                  {group.name}
                </option>
              ))}
            </select>
          </Field>

          <Field label="邮箱已存在时">
            <select
              className="min-h-9 w-full rounded-md border border-kumo-line bg-kumo-base px-2 text-sm"
              value={onConflict}
              onChange={(event) => setOnConflict(event.target.value)}
            >
              <option value="skip">跳过</option>
              <option value="update">更新凭据</option>
            </select>
            <p className="mt-1 text-xs text-kumo-subtle">
              更新模式会保留已有的分组、备注与代理设置，只覆盖凭据。
            </p>
          </Field>
        </aside>
      </form>

      {result && <ImportSummary result={result} />}
    </PageShell>
  );
}

function Field({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <label className="block text-sm font-medium text-kumo-default">
      {label}
      <div className="mt-2 font-normal">{children}</div>
    </label>
  );
}

// 导入是逐行统计的，四个计数要一起展示——只说「成功 N 个」会让用户
// 不知道剩下的去哪了。
function ImportSummary({ result }: { result: ImportResult }) {
  return (
    <section className="mt-8 rounded-lg border border-kumo-line bg-kumo-elevated p-5">
      <h2 className="text-lg font-medium">导入结果</h2>
      <dl className="mt-4 grid grid-cols-2 gap-4 sm:grid-cols-5">
        <Stat label="总行数" value={result.total} />
        <Stat label="新建" value={result.created} />
        <Stat label="更新" value={result.updated} />
        <Stat label="跳过" value={result.skipped} />
        <Stat label="失败" value={result.failed} />
      </dl>
      {result.errors.length > 0 && (
        <div className="mt-5">
          <h3 className="text-sm font-medium">逐行说明</h3>
          <ul className="mt-2 max-h-64 overflow-y-auto rounded-md bg-kumo-recessed p-3 text-xs">
            {result.errors.map((e) => (
              <li key={`${e.line}-${e.email}`} className="py-0.5">
                <span className="text-kumo-subtle">第 {e.line} 行</span>{" "}
                {e.email && <span className="font-mono">{e.email}</span>} — {e.reason}
              </li>
            ))}
          </ul>
          {result.truncated && (
            <p className="mt-2 text-xs text-kumo-subtle">仅显示前 200 条，其余已省略。</p>
          )}
        </div>
      )}
    </section>
  );
}

function Stat({ label, value }: { label: string; value: number }) {
  return (
    <div>
      <dt className="text-xs text-kumo-subtle">{label}</dt>
      <dd className="mt-1 text-xl font-semibold tabular-nums">{value}</dd>
    </div>
  );
}
