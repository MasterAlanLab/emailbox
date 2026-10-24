import { useEffect } from "react";
import { Link, useLocation } from "react-router-dom";
import { PageShell } from "@/components/layout/PageShell";
import { SUPPORT_EMAIL } from "@/lib/support";

export default function TermsPage() {
  const { hash } = useLocation();
  useEffect(() => {
    // 站内路由不会触发浏览器的整页锚点定位，购买入口需直接落在对应规则上。
    if (hash) document.getElementById(hash.slice(1))?.scrollIntoView();
  }, [hash]);

  return (
    <PageShell title="服务条款" description="最后更新：2026 年 10 月 24 日">
      <article className="max-w-2xl space-y-8 text-sm leading-7 text-kumo-subtle">
        <p>
          使用 Emailbox 前，请阅读以下约定，尤其是
          <Link className="text-kumo-link underline" to="#refunds">
            退款政策
          </Link>
          、
          <Link className="text-kumo-link underline" to="#liability">
            免责与责任限制
          </Link>
          和
          <Link className="text-kumo-link underline" to="#breach">
            违约处理
          </Link>
          。 如对条款有疑问，请通过下方联系邮箱咨询后再决定是否使用或订阅。
        </p>
        <section>
          <h2 className="font-medium text-kumo-strong">服务内容</h2>
          <p className="mt-2">
            本平台托管你提供的第三方邮箱连接凭据，根据你的操作或定时任务设置连接邮件服务商，
            提供统一收信、令牌刷新、代理配置与批量运维。平台不提供代发邮件服务。 数据处理方式见
            <Link className="text-kumo-link underline" to="/legal/privacy-policy">
              隐私政策
            </Link>
            。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">账号授权与使用限制</h2>
          <ul className="mt-2 list-disc space-y-2 pl-5">
            <li>
              只可导入属于你或已获所有者明确授权的邮箱，不得托管、访问或导出未获授权的邮箱凭据和邮件。
            </li>
            <li>
              遵守邮件服务商的访问频率与授权规则，不得通过过度频繁刷新、批量请求或绕过限流干扰服务、规避风控。
            </li>
            <li>
              不得用于盗号、欺诈、侵犯隐私或其他违法活动，不得尝试访问其他用户的数据或破坏平台安全措施。
            </li>
            <li>
              妥善保管登录密码、API Key 和导出文件；发现泄露应及时更改密码、重置
              Key，并联系我们处理。
            </li>
          </ul>
          <p className="mt-2">
            上游服务商可能要求重新授权、限制访问或封禁邮箱。付费订阅和平台限流均不保证免受上游风控，
            请根据实际用途设置合理的刷新频率。
          </p>
        </section>
        <section id="billing" className="scroll-mt-20">
          <h2 className="font-medium text-kumo-strong">计费与订阅</h2>
          <p className="mt-2">
            免费套餐在所列额度内不收费，未支付费用的免费使用不涉及退款。
            当前免费范围、付费档位、币种和月付或年付价格均在
            <Link className="text-kumo-link underline" to="/pricing">
              价格与额度
            </Link>
            页面公开展示。 支付入口关闭仅表示暂停新购买，不会自动取消已有订阅。
          </p>
          <p className="mt-2">
            付费服务通过 Waffo Pancake 结账，按所选月度或年度周期收取费用，年付一次收取全年费用。
            提交订阅付款即授权按结账页面列明的金额和周期自动续费，直至取消。
            请在确认付款前核对最终金额、币种及结账页面列明的税费。
          </p>
          <p className="mt-2">
            可在「套餐与用量」选择「到期后取消」，取消后保留已付费周期的服务，到期不再续费。
            取消续费不等于退款，退款按下一节处理。套餐或价格变更以购买时确认的订单及后续有效通知为准，
            不追溯增加已支付周期的费用。
          </p>
          <p className="mt-2">
            网页与 API
            共用每日取件额度。令牌刷新不设每日次数配额，但仍受任务执行频率与邮件服务商限制；
            不得将未设次数上限理解为可以无限频率请求。
          </p>
        </section>
        <section id="refunds" className="scroll-mt-20">
          <h2 className="font-medium text-kumo-strong">退款政策</h2>
          <ul className="mt-2 list-disc space-y-2 pl-5">
            <li>重复扣款或错误收费：核实后退还重复收取的款项或错误差额。</li>
            <li>
              因平台原因未能提供已付费服务：可申请退还未履行部分的费用，按未履行时长占对应已付费周期的比例核算。
            </li>
            <li>
              已正常提供的服务，不因主动取消、未使用或使用量较少而按剩余额度或剩余时间退款。法律另有规定的除外。
            </li>
          </ul>
          <p className="mt-2">
            申请方式：发送邮件至
            <a className="text-kumo-link underline" href={`mailto:${SUPPORT_EMAIL}`}>
              {SUPPORT_EMAIL}
            </a>
            ， 提供平台用户名、付款邮箱、订单编号、付款时间与金额，以及申请原因和必要凭证。
            请勿发送邮箱密码、访问令牌、API Key 或完整银行卡信息。
          </p>
          <p className="mt-2">
            我们会在收到申请后 3
            个工作日内首次回复，核实付款归属、适用条件和退款金额，并通过邮件告知处理结果。
            符合条件的退款经支付服务商退回原支付渠道；到账时间受支付渠道和银行处理时间影响。
            如退款涉及停止剩余服务，我们会一并告知权益结束时间；取消与退款需分别确认处理结果。
            本政策不限制适用法律赋予你的退款或其他救济权利。
          </p>
        </section>
        <section id="liability" className="scroll-mt-20">
          <h2 className="font-medium text-kumo-strong">免责与责任限制（重要）</h2>
          <p className="mt-2">
            服务按现状和可用状态提供。我们会采取合理措施维护平台和保护凭据，
            但不保证服务永不中断、完全无错误，或适合你的所有特定用途。
            邮件服务商的政策、接口、授权状态、网络及你配置的代理均可能影响连接和取件结果。
          </p>
          <p className="mt-2">
            对于因你未获授权的使用、违反服务商规则或自行泄露凭据而产生，且非平台过错造成的损失，
            由责任方依法承担。平台仍对自身违约或过错造成的损失依法负责；
            第三方故障不免除我们应承担的通知、协助处理及本条款约定的退款义务。
          </p>
          <p className="mt-2 font-medium text-kumo-strong">
            在适用法律允许的范围内，平台就服务承担的普通直接损失赔偿总额， 以引发索赔事件发生前 12
            个月内你为相关服务实际支付的费用为上限。
            此上限不适用于人身损害、我们故意或重大过失造成的损失，以及依法不得限制或免除的责任，
            也不减损本条款的退款义务和你的法定消费者权利。
          </p>
        </section>
        <section id="breach" className="scroll-mt-20">
          <h2 className="font-medium text-kumo-strong">违约责任与服务终止</h2>
          <p className="mt-2">
            发现违反授权、频率或安全规则的行为时，我们可按风险与严重程度采取以下措施：
          </p>
          <ol className="mt-2 list-decimal space-y-2 pl-5">
            <li>对一般且可纠正的问题，提醒说明原因并要求限期整改。</li>
            <li>
              对过度频繁刷新、绕过限流或影响其他用户的行为，限制配额、请求频率或暂停相关功能。
            </li>
            <li>
              对导入未获授权邮箱、持续触发风控或存在凭据泄露风险的行为，暂停相关账号或服务，要求提供授权证明并配合处理。
            </li>
            <li>
              对严重违法、攻击平台或反复违规且拒不整改的行为，终止服务或关闭账户，并依法配合有权机关处理。
            </li>
          </ol>
          <p className="mt-2">
            涉及紧急安全风险时可先采取限制措施，再说明原因。你可通过下方联系邮箱提交解释或申诉，
            我们会核实后告知处理结果；限制措施应与风险相称。因违规造成平台或第三方实际损失的，
            你应按依法认定的过错和责任承担赔偿，平台不据此收取任意罚款。
          </p>
          <p className="mt-2">
            服务暂停或终止不免除已发生的付款、退款及法定责任。依法可退的款项仍按退款政策处理，
            不因关闭账户而当然没收；可依法提供的数据移交与清理由我们协助核实处理。
          </p>
        </section>
        <section>
          <h2 className="font-medium text-kumo-strong">变更与联系</h2>
          <p className="mt-2">
            条款变更会更新本页日期。涉及收费、退款或责任范围的重大变更，会在生效前通过站内公告或可用联系渠道提示，
            不追溯减损已确认订单的权益。退款、账单争议、授权问题及申诉请联系
            <a className="text-kumo-link underline" href={`mailto:${SUPPORT_EMAIL}`}>
              {SUPPORT_EMAIL}
            </a>
            。
          </p>
        </section>
      </article>
    </PageShell>
  );
}
