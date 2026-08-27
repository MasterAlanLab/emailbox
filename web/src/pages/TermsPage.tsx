import { SettingsPage } from "./ProfileSettingsPage";
export default function TermsPage() {
  return (
    <SettingsPage title="服务条款" description="最后更新：2026 年 8 月 20 日">
      <article className="max-w-2xl space-y-8 text-sm leading-7 text-kumo-subtle">
        <section>
          <h2 className="font-medium text-kumo-strong">服务内容</h2>
          <p className="mt-2">
            本平台为你托管第三方邮箱账号的连接凭据，并在你发起请求时代你连接对应的邮件服务商，
            用于收取邮件、刷新访问令牌等操作。平台不代你发送邮件，也不主动读取你未请求的内容。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">账号授权（重要）</h2>
          <p className="mt-2">
            你必须对托管到本平台的每一个邮箱账号拥有合法授权——它属于你，或其所有者明确授权你使用。
            导入不属于你且未获授权的账号，既违反本条款，也可能违反邮件服务商的服务协议与所在地法律。
            我们会在你发现问题时配合处理，但责任由导入者承担。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">使用限制</h2>
          <p className="mt-2">
            请遵守各邮件服务商的服务条款，包括其对访问频率的限制。过于激进的批量刷新可能触发
            服务商风控并导致账号被封禁，这类损失平台无法挽回。配额限制的存在正是为了降低这种风险。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">账户责任</h2>
          <p className="mt-2">
            请妥善保管登录密码，并对账号内的操作负责。导出凭据需要二次输入登录密码，
            且每一次导出都会记入审计日志。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">变更</h2>
          <p className="mt-2">条款发生变化时，本页面会同步更新并调整上方的更新日期。</p>
        </section>
      </article>
    </SettingsPage>
  );
}
