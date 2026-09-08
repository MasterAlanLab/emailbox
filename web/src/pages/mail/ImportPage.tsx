import { Button } from "@cloudflare/kumo/components/button";
import { Textarea } from "@cloudflare/kumo/components/input";
import { useEffect, useState } from "react";
import { mailApi, type ImportResult, type MailGroupNode } from "@/api/mail";
import { PageShell } from "@/components/layout/PageShell";
import { findSystemGroup } from "@/components/mail/groupOptions";
import { useAsyncAction } from "@/lib/useAsyncAction";
import { useTenantStore } from "@/store/tenantStore";

// 每种格式都带一段**多行**样例，直接当 placeholder 用。
// 这个输入框有 16 行高，空着的时候是页面上最大的一块留白；只放一条 Outlook 的样例，
// 用 QQ / 163 / Gmail 的人会以为这里只收 Outlook。
const FORMATS = [
  {
    value: "auto",
    label: "自动识别",
    hint: "按段数与字段形态判断，混合内容也能处理",
    placeholder: [
      "zhang@qq.com----授权码",
      "li@163.com----授权码",
      "wang@gmail.com----应用专用密码",
      "alice@outlook.com----密码----client_id----refresh_token",
      "bob@example.com----密码----imap.example.com----993",
    ],
  },
  {
    value: "outlook_oauth",
    label: "Outlook OAuth（4 段）",
    hint: "邮箱----密码----client_id----refresh_token",
    placeholder: [
      "alice@outlook.com----密码----client_id----refresh_token",
      "bob@hotmail.com----密码----client_id----refresh_token",
    ],
  },
  {
    value: "imap",
    label: "标准 IMAP（2 段）",
    hint: "邮箱----授权码，服务商按域名推断",
    placeholder: [
      "zhang@qq.com----授权码",
      "li@163.com----授权码",
      "wang@gmail.com----应用专用密码",
    ],
  },
  {
    value: "custom_imap",
    label: "自定义 IMAP（4 段）",
    hint: "邮箱----密码----imap 服务器----端口",
    placeholder: [
      "bob@example.com----密码----imap.example.com----993",
      "carol@example.net----密码----mail.example.net----993",
    ],
  },
];

// 这份清单是后端 `domainProvider`（pkg/mailer/provider.go）的用户可见版本，
// 加服务商时两处都要改。之所以值得多维护一份：域名认不认识决定了 2 段写法能不能用，
// 而用户在这里唯一的替代方案是先导一批进去、失败了再回来猜。
//
// 最后一行的「其他域名」用 RFC 2606 保留的 example.com，不要换成看起来更像真企业域的
// 名字：那些域名多半真有人注册（corp.com 就是微软的），照抄不改的人会去连别人的服务器。
const SAMPLES = [
  { label: "QQ / Foxmail", line: "zhang@qq.com----授权码" },
  { label: "163 / 126", line: "li@163.com----授权码" },
  { label: "Gmail", line: "wang@gmail.com----应用专用密码" },
  { label: "Yahoo", line: "kate@yahoo.com----应用专用密码" },
  { label: "阿里 / 2925", line: "zhao@aliyun.com----授权码" },
  { label: "Outlook / Hotmail", line: "alice@outlook.com----密码----client_id----refresh_token" },
  { label: "其他域名", line: "bob@example.com----密码----imap.example.com----993" },
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
  const current = FORMATS.find((f) => f.value === format) ?? FORMATS[0];

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
    <PageShell title="批量导入邮箱" description="支持按行批量解析，字段之间使用 ---- 分隔。">
      {/* 服务条款要求在导入页展示一次授权提示 */}
      <div className="mb-6 rounded-lg border border-kumo-line bg-kumo-warning-tint p-4 text-sm">
        授权承诺：导入即表示你确认对所填邮箱账号拥有合法访问权限。凭据将在落库前执行 AES-256-GCM 独立加密，严禁托管未获授权的第三方凭据。
      </div>

      <form onSubmit={submit} className="grid gap-6 lg:grid-cols-[1fr_320px]">
        <div className="space-y-4">
          <Textarea
            label={`账号内容${lineCount > 0 ? `（${lineCount} 行）` : ""}`}
            rows={16}
            className="font-mono text-xs"
            placeholder={current.placeholder.join("\n")}
            value={content}
            onChange={(event) => setContent(event.target.value)}
            required
          />
          {error && <p className="text-sm text-kumo-danger">{error}</p>}
          <Button type="submit" variant="secondary" size="lg" disabled={pending || !content.trim()}>
            {pending ? "导入中…" : `导入 ${lineCount} 行`}
          </Button>

          <FormatSamples />
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
            <p className="mt-1 text-xs text-kumo-subtle">{current.hint}</p>
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

// 摆在输入框正下方，而不是折叠进侧栏：会来看示例的人，正是那些还不确定自己
// 这批账号该写成几段的人，让他们先点开一层才看得到没有道理。
function FormatSamples() {
  return (
    <section className="rounded-lg border border-kumo-line bg-kumo-elevated p-5">
      <h2 className="text-sm font-medium text-kumo-strong">格式示例</h2>
      <div className="mt-3 overflow-x-auto">
        <table className="w-full text-left text-sm">
          <tbody className="divide-y divide-kumo-hairline">
            {SAMPLES.map((s) => (
              <tr key={s.label}>
                <td className="py-2 pr-4 align-top whitespace-nowrap text-kumo-subtle">
                  {s.label}
                </td>
                <td className="py-2 align-top font-mono text-xs whitespace-nowrap">{s.line}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <ul className="mt-4 space-y-1.5 text-xs text-kumo-subtle">
        <li>
          QQ、163、126、Gmail、Yahoo、阿里填的是邮箱网页版「设置 → 账号 / POP3·IMAP」里开启 IMAP
          服务后生成的授权码或应用专用密码，<strong className="font-medium">不是登录密码</strong>
          ；用登录密码会以「授权码错误，或未在邮箱设置中开启 IMAP 服务」失败。
        </li>
        <li>
          Outlook / Hotmail 只能走 4 段 OAuth：微软已停用个人账号的邮箱密码登录，2
          段写法导得进来也刷不动信。
        </li>
        <li>
          域名不在上面这些里（企业自建域等），用最后一行的 4 段写法显式给出 IMAP 服务器与端口。
        </li>
      </ul>
    </section>
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
        <Stat label="新增账号" value={result.created} />
        <Stat label="凭据更新" value={result.updated} />
        <Stat label="重复跳过" value={result.skipped} />
        <Stat label="格式错误" value={result.failed} />
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
