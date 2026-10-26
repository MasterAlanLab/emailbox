import { useEffect } from "react";
import { Link, useLocation } from "react-router-dom";
import { PageShell } from "@/components/layout/PageShell";
import { SUPPORT_EMAIL } from "@/lib/support";

export default function PrivacyPage() {
  const { hash } = useLocation();
  useEffect(() => {
    // 站内路由不会触发浏览器的整页锚点定位，审核与条款入口需直接落在对应规则上。
    if (hash) document.getElementById(hash.slice(1))?.scrollIntoView();
  }, [hash]);

  return (
    <PageShell title="隐私政策" description="最后更新：2026 年 10 月 26 日">
      <article className="max-w-2xl space-y-8 text-sm leading-7 text-kumo-subtle">
        <p>
          本政策说明 mail.corouter.cc 上的 Emailbox 如何处理账号信息与邮件数据。使用前请阅读
          <Link className="text-kumo-link underline" to="#sharing">
            第三方共享
          </Link>
          和
          <Link className="text-kumo-link underline" to="#rights">
            用户数据权利
          </Link>
          。自建部署的适用范围见文末。
        </p>
        <section>
          <h2 className="font-medium text-kumo-strong">我们存储的信息</h2>
          <p className="mt-2">
            账户信息：用户名、邮箱、密码哈希与会话记录。
            邮箱托管信息：你导入的邮箱地址、备注、分组标签，以及连接所需的凭据（登录密码、OAuth
            refresh_token、代理地址）。付费订阅时还会保存订单编号、套餐、金额、币种、订阅状态和计费周期，不保存完整银行卡号或支付密码。
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
            默认不在本地保留邮件正文。每次查看都是实时向服务商拉取，经我们的服务器转到你的浏览器，不写入数据库。
            正文在渲染前会被净化并隔离在沙箱中，远程图片默认阻断，避免追踪像素泄露你的阅读行为。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">谁能看到你的数据</h2>
          <p className="mt-2">
            不同用户之间完全隔离，其他用户无法访问你的账号与邮件。
            平台管理员出于运维需要可以通过专用管理接口跨用户访问，使用的是管理员自己的会话，
            而不是你的登录身份。管理员不是下一节所说的第三方。
          </p>
        </section>
        <section id="sharing" className="scroll-mt-20">
          <h2 className="font-medium text-kumo-strong">第三方共享</h2>
          <p className="mt-2">
            除连接你指定的邮件服务商，以及下面列明的委托处理外，我们不出售账号信息或邮件数据，
            也不将其提供给第三方做广告或独立商业利用。
          </p>
          <ul className="mt-2 list-disc space-y-2 pl-5">
            <li>
              邮件服务商：为完成你发起的收信、令牌刷新或有效性检测，我们使用你保存的凭据连接你指定的服务商（如
              Outlook、Gmail、QQ、163）。该连接只把协议所必需的凭据和请求发给该服务商。邮件正文从该服务商实时取回，不写入我们的数据库，也不会再交给其他邮件服务商。
            </li>
            <li>
              支付服务商：仅在你购买付费套餐时，我们把结账所必需的信息交给 Waffo（Waffo
              Pancake）处理付款与订阅。包括资料中的联系邮箱（只用于预填结账页；没有则由你在结账页填写）、内部租户标识、所选套餐与订单编号、金额与币种。不包括托管邮箱地址清单、登录密码、OAuth
              refresh_token、代理口令或邮件正文。银行卡等支付账户信息在 Waffo
              的结账页填写，我们不接收、不存储完整卡号或支付密码。订阅状态、订单号、金额和周期留在我们这边，用于开通权益和核对退款。
            </li>
            <li>
              云托管服务商：账号信息、凭据密文和订阅记录存放在我们自行租用并运营的云服务器上，数据库和加密密钥由我们管理。云服务器服务商只提供机器、磁盘和网络，不接收我们另行提交的数据副本，也拿不到解密密钥。邮件正文不写入该数据库。我们不把这些数据交给云厂商做分析、广告或转售，也不委托其他云托管服务商代为保管。
            </li>
            <li>
              CDN 与 WAF：Cloudflare 只用作 CDN 和 WAF，不存放业务数据库。CDN
              分发网站静态资源，不缓存邮件正文、凭据或账户数据。WAF
              检查访问请求以拦截攻击。由于访问经过
              Cloudflare，登录、查看邮件和导出文件时的请求与响应会在传输过程中经过它的网络。我们不把数据库或邮件存档同步到
              Cloudflare，不向其提供解密密钥，也不使用它的统计分析产品，不授权其将这些数据用于广告或转售。
            </li>
            <li>
              统计分析服务商：我们不接入第三方统计分析、广告或错误上报服务，不会把账号信息或邮件数据提供给统计分析服务商。运行日志只记录请求方法、路径、状态码、耗时和
              request_id，用于排障，不记录密码、令牌或邮件正文。任务记录与令牌刷新日志保留 30
              天，任务事件默认保留 7 天，到期删除。
            </li>
            <li>
              你配置的代理：如果分组填写了代理地址，连接邮件服务商的流量会经过该代理。代理由你指定，不是我们委托的服务商。不填代理则不经过第三方代理。
            </li>
            <li>依法提供：法律法规或有权机关依法要求时，我们仅在要求的范围内提供。</li>
          </ul>
        </section>
        <section id="rights" className="scroll-mt-20">
          <h2 className="font-medium text-kumo-strong">用户数据权利</h2>
          <p className="mt-2">
            你可以访问、更正、删除、导出自己的数据，并撤回授权。能在产品里完成的，登录后按下列入口操作；做不到的，通过本节末尾的渠道申请。请不要在邮件里发送邮箱密码、刷新令牌、API
            Key 或完整银行卡信息。
          </p>
          <ul className="mt-2 list-disc space-y-2 pl-5">
            <li>
              访问：在「个人资料」查看用户名和联系邮箱；在「邮箱」查看托管账号、分组与备注，并实时查看邮件；在「用量」查看套餐、额度与订阅状态。邮件正文不在我们这里存档，打开时向服务商拉取。
            </li>
            <li>
              更正：在「个人资料」修改用户名和联系邮箱，在「账号安全」修改密码。托管账号的备注、分组、代理和凭据可在「邮箱」中修改。
            </li>
            <li>
              删除：在「邮箱」选中托管账号后删除。该账号的登录密码、刷新令牌和代理地址密文会被物理清除，不能恢复。删除分组只移除分组，其中的账号回到默认分组，凭据仍在。在邮件列表中删除邮件，会向对应邮件服务商发出删除请求，不是删除我们保存的副本。
            </li>
            <li>
              关闭平台账户没有自助入口。请发邮件申请，写明用户名，并说明要关闭账户。核实后我们会停用账户、清除登录会话，并物理清除该账户下全部托管邮箱的凭据密文。关闭后不能再登录。账户名、联系邮箱，以及退款和争议所需的订单记录，会在处理争议和法定保存所需的范围内保留，不再用于提供服务。
            </li>
            <li>
              导出：在「邮箱」使用「导出」，可下载托管账号的密码与刷新令牌明文，格式与导入一致。导出文件由你保管。我们不存储邮件正文，因此不能从本平台导出历史邮件；需要留存邮件请在邮件服务商处操作。个人资料和订阅状态可在对应页面查看。如需我们提供一份账户信息副本，可按下面的渠道申请。
            </li>
            <li>
              撤回授权：删除对应托管账号，即停止我们使用其凭据。使用 OAuth 的邮箱（如
              Outlook、Gmail），请同时在该服务商的账户安全设置中撤销对本应用的授权；只在这里删除，服务商侧尚未过期的授权不会自动失效。在「用量」选择「到期后取消」，可撤回下一周期的扣款授权，这不删除已有数据。在「API」重置
              Key，旧 Key
              立即失效。修改密码会使已有登录会话失效。关闭账户即撤回我们继续处理该账户数据的授权，法定保存的记录除外。
            </li>
          </ul>
          <p className="mt-2">
            行使权利的渠道：登录后使用上面的页面。需要我们协助关闭账户、提供数据副本，或对访问、更正、删除、导出有争议的，发送邮件至
            <a className="text-kumo-link underline" href={`mailto:${SUPPORT_EMAIL}`}>
              {SUPPORT_EMAIL}
            </a>
            。请写明用户名、要行使的权利和涉及的账号。我们会在收到后 3
            个工作日内首次回复，核实身份后办理，并邮件告知结果。需要更长时间的，会说明原因和预计完成时间。本政策不限制适用法律赋予你的其他权利。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">自建部署</h2>
          <p className="mt-2">
            若你自行部署本项目，数据存放在部署者自己的数据库中。是否使用支付服务商、云托管服务商或统计分析服务商，以及由谁处理用户请求，由部署者决定，并应另行说明。上文关于自有云服务器、Waffo、Cloudflare
            和 {SUPPORT_EMAIL} 的安排，只适用于 mail.corouter.cc 上的公开服务。
          </p>
        </section>
      </article>
    </PageShell>
  );
}
