import { SettingsPage } from "./ProfileSettingsPage";
export default function PrivacyPage() {
  return (
    <SettingsPage title="隐私政策" description="最后更新：2026 年 8 月 20 日">
      <article className="max-w-2xl space-y-8 text-sm leading-7 text-kumo-subtle">
        <section>
          <h2 className="font-medium text-kumo-strong">我们存储的信息</h2>
          <p className="mt-2">
            账户信息：用户名、邮箱、密码哈希与会话记录。
            邮箱托管信息：你导入的邮箱地址、备注、分组标签，以及连接所需的凭据 （登录密码、OAuth
            refresh_token、代理地址）。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">凭据如何保管</h2>
          <p className="mt-2">
            凭据以 AES-256-GCM 加密后落库，每条记录使用独立随机 nonce，密钥不与数据库存放在一起。
            列表与详情接口只回传「是否已设置」这类标志，明文只在两种情况下离开服务器：
            你主动导出，或平台代你连接邮件服务商。账户被删除时凭据密文会被物理清除，而非仅标记删除。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">邮件内容</h2>
          <p className="mt-2">
            默认不在本地保留邮件正文——每次查看都是实时向服务商拉取。
            正文在渲染前会被净化并隔离在沙箱中，远程图片默认阻断，避免追踪像素泄露你的阅读行为。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">谁能看到你的数据</h2>
          <p className="mt-2">
            不同用户之间完全隔离，其他用户无法访问你的账号与邮件。
            平台管理员出于运维需要可以跨用户访问，但每一次此类访问都会记入审计日志，
            包括操作者、时间与来源 IP。平台不提供「以你的身份登录」的功能。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">自建部署</h2>
          <p className="mt-2">
            若你自行部署本项目，上述数据全部存放在你自己的数据库中，
            数据安全、备份与用户请求处理由部署者负责。
          </p>
        </section>
      </article>
    </SettingsPage>
  );
}
