import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import {
  pricingApi,
  type PublicPlan,
  type PublicPlanPrice,
  type PublicPricing,
} from "@/api/pricing";
import { PageShell } from "@/components/layout/PageShell";
import { useAuthStore } from "@/store/authStore";

type PricingState = { status: "loading" | "error" } | { status: "ready"; data: PublicPricing };

export default function PricingPage() {
  const authed = useAuthStore((state) => state.isAuthenticated);
  const [state, setState] = useState<PricingState>({ status: "loading" });
  const [reloadKey, setReloadKey] = useState(0);

  useEffect(() => {
    let ignore = false;
    void pricingApi
      .get()
      .then((response) => {
        if (ignore) return;
        if (response.code !== 0) {
          setState({ status: "error" });
          return;
        }
        setState({ status: "ready", data: response.data });
      })
      .catch(() => {
        if (!ignore) setState({ status: "error" });
      });
    return () => {
      ignore = true;
    };
  }, [reloadKey]);

  return (
    <PageShell title="价格与额度" description="先了解免费使用范围，再选择适合你的邮箱托管额度。">
      {state.status === "loading" && (
        <p role="status" className="text-sm text-kumo-subtle">
          正在加载当前价格与额度…
        </p>
      )}
      {state.status === "error" && (
        <div className="space-y-3">
          <p role="alert" className="text-sm text-kumo-danger">
            价格与额度加载失败，请检查网络后重试。
          </p>
          <Button
            variant="secondary"
            onClick={() => {
              setState({ status: "loading" });
              setReloadKey((key) => key + 1);
            }}
          >
            重新加载
          </Button>
        </div>
      )}
      {state.status === "ready" && (
        <>
          <p className="mb-6 text-sm leading-6 text-kumo-subtle">
            {state.data.billing_enabled
              ? state.data.paid_plans.length > 0
                ? "免费套餐在所列额度内不收费。付费套餐按月或按年订阅，价格与计费周期如下。"
                : "当前暂无可购买的付费套餐，可先按下方免费套餐额度使用。"
              : "当前付费购买暂未开放，可先按下方免费套餐额度使用。"}
          </p>
          <div className="grid items-start gap-5 md:grid-cols-2 xl:grid-cols-3">
            <LayerCard className="min-w-0 p-5 sm:p-6">
              <h2 className="text-lg font-semibold wrap-anywhere text-kumo-strong">
                {state.data.free_plan.name}
              </h2>
              <p className="mt-5 text-3xl font-semibold text-kumo-strong">免费</p>
              <p className="mt-2 text-sm text-kumo-subtle">在以下额度内使用，无需付款</p>
              <PlanLimits plan={state.data.free_plan} />
              <LinkButton
                className="mt-6 w-full"
                href={authed ? "/mail" : "/register"}
                variant="secondary"
              >
                {authed ? "进入控制台" : "免费注册体验"}
              </LinkButton>
            </LayerCard>
            {state.data.paid_plans.map((plan, index) => (
              <LayerCard key={index} className="min-w-0 p-5 sm:p-6">
                <h2 className="text-lg font-semibold wrap-anywhere text-kumo-strong">
                  {plan.name}
                </h2>
                <div className="mt-5 space-y-4">
                  {plan.prices.map((price) => (
                    <PlanPrice key={`${price.billing_period}-${price.currency}`} price={price} />
                  ))}
                </div>
                <PlanLimits plan={plan} />
                {state.data.billing_enabled ? (
                  <LinkButton
                    className="mt-6 w-full"
                    href={authed ? "/settings/usage" : "/register"}
                    variant="secondary"
                  >
                    {authed ? "查看订阅选项" : "注册后订阅"}
                  </LinkButton>
                ) : (
                  <p className="mt-6 text-sm text-kumo-subtle">付费购买暂未开放</p>
                )}
              </LayerCard>
            ))}
          </div>
          <section className="mt-10 max-w-3xl border-t border-kumo-line pt-6 text-sm leading-7 text-kumo-subtle">
            <h2 className="mb-3 font-medium text-kumo-strong">计费与使用说明</h2>
            <ul className="list-disc space-y-2 pl-5">
              <li>网页与 API 共用每日取件额度；列出邮件、读取正文均计入该额度。</li>
              <li>令牌刷新不扣每日取件次数，仍受平台任务频率控制及邮件服务商的访问限制。</li>
              <li>
                付费订阅按所选周期自动续费。年费每次收取全年费用，可在「套餐与用量」取消下一周期续费。
              </li>
              <li>
                购买前请阅读{" "}
                <Link className="text-kumo-link underline" to="/legal/terms#billing">
                  计费条款
                </Link>{" "}
                与{" "}
                <Link className="text-kumo-link underline" to="/legal/terms#refunds">
                  退款政策
                </Link>
                。实际付款金额和币种以结账页面为准。
              </li>
            </ul>
          </section>
        </>
      )}
    </PageShell>
  );
}

function PlanLimits({ plan }: { plan: PublicPlan }) {
  return (
    <dl className="mt-6 space-y-3 border-t border-kumo-line pt-5 text-sm">
      {[
        { label: "托管邮箱", limit: plan.max_accounts, unit: "个" },
        { label: "邮箱分组", limit: plan.max_groups, unit: "个" },
        { label: "每日取件", limit: plan.daily_mail_fetch, unit: "次" },
      ].map(({ label, limit, unit }) => (
        <div key={label} className="flex flex-wrap justify-between gap-x-4 gap-y-1">
          <dt className="text-kumo-subtle">{label}</dt>
          <dd className="font-medium text-kumo-strong">
            {limit === -1 ? "不限" : `${limit.toLocaleString("zh-CN")} ${unit}`}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function PlanPrice({ price }: { price: PublicPlanPrice }) {
  const yearly = price.billing_period === "yearly";
  return (
    <div>
      <p className="flex flex-wrap items-baseline gap-x-2 gap-y-1 text-kumo-strong">
        <span className="min-w-0 text-3xl font-semibold wrap-anywhere">{price.amount}</span>
        <span className="text-sm wrap-anywhere">
          {price.currency} / {yearly ? "年" : "月"}
        </span>
      </p>
      <p className="mt-1 text-xs text-kumo-subtle">
        {yearly ? "按年计费，每次支付全年费用" : "按月计费，每次支付一个月费用"}
      </p>
    </div>
  );
}
