import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { useEffect, useState } from "react";
import { tenantApi, type BillingPlanPrice, type BillingSubscription } from "@/api/tenant";
import { useAsyncAction } from "@/lib/useAsyncAction";

const STATUS_LABEL: Record<BillingSubscription["status"], string> = {
  pending: "处理中",
  active: "生效中",
  canceling: "周期末取消",
  past_due: "续费失败",
  canceled: "已结束",
  expired: "已过期",
};

// 仍占着「当前订阅」位置的状态，与后端 blockingSubscriptionStatus 一致。
// 已结束的订阅不挡购买：用户取消后重新订阅是最常见的回流路径。
const BLOCKING: BillingSubscription["status"][] = ["pending", "active", "canceling", "past_due"];

interface BillingCardProps {
  tenantID: string;
  /** 变化时重取订阅；用量页的「刷新」同时驱动配额与这里。 */
  reloadKey: number;
  /** 付款回跳时为 true：Webhook 可能还没到，提示用户稍后刷新。 */
  returnedFromCheckout: boolean;
  onRefresh: () => void;
  /** 注入跳转，测试里不必真的离开页面。 */
  navigate?: (url: string) => void;
}

// 订阅卡片。权益以 Webhook 为准：回跳页只提示「处理中」，不根据 URL 参数改任何状态。
export function BillingCard({
  tenantID,
  reloadKey,
  returnedFromCheckout,
  onRefresh,
  navigate = (url) => window.location.assign(url),
}: BillingCardProps) {
  const [prices, setPrices] = useState<BillingPlanPrice[]>([]);
  const [subscription, setSubscription] = useState<BillingSubscription | null>(null);
  const [loaded, setLoaded] = useState(false);
  const { error, pending, run } = useAsyncAction();

  // 取订阅走对账接口：Webhook 到不了（本地开发、回调丢失）时，付完款回到这一页
  // 也能看到新套餐。套餐变了就让用量页重取配额；再次对账时 changed 为 false，不会来回触发。
  useEffect(() => {
    let ignore = false;
    void Promise.all([tenantApi.billingPlans(tenantID), tenantApi.syncBilling(tenantID)])
      .then(([plans, sync]) => {
        if (ignore) return;
        setPrices(plans.data);
        setSubscription(sync.data.subscription);
        setLoaded(true);
        if (sync.data.changed) onRefresh();
      })
      // 读不到计费信息只是少了购买入口，不该拖垮整个用量页。
      .catch(() => {
        if (!ignore) setLoaded(true);
      });
    return () => {
      ignore = true;
    };
    // onRefresh 是父组件每次渲染新建的回调，放进依赖会让每次渲染都重新对账。
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [tenantID, reloadKey]);

  const current = subscription && BLOCKING.includes(subscription.status) ? subscription : null;
  if (!loaded || (prices.length === 0 && !current)) return null;

  // 每次点击一个新键；连点由 useAsyncAction 的在途锁挡住。
  // 同一个键重发（网络重试）时服务端复用会话，不会建出第二个结账。
  const checkout = (priceID: string) =>
    void run(async () => {
      const resp = await tenantApi.checkout(tenantID, {
        plan_price_id: priceID,
        idempotency_key: crypto.randomUUID(),
      });
      // 同页跳转而不是 window.open：await 之后的 open 已不算用户手势，会被拦截。
      // 付款完成后 Waffo 的「完成」按钮会带用户回到这里。
      navigate(resp.data.checkout_url);
    });

  const setCancel = (cancel: boolean) =>
    void run(async () => {
      if (cancel) await tenantApi.cancelBilling(tenantID);
      else await tenantApi.uncancelBilling(tenantID);
      onRefresh();
    });

  return (
    <LayerCard className="mt-6 max-w-5xl p-5">
      <div className="flex flex-wrap items-center gap-3">
        <h2 className="text-lg font-medium">订阅</h2>
        {current && <Badge variant="neutral">{STATUS_LABEL[current.status]}</Badge>}
        <Button className="ml-auto" size="sm" variant="secondary" onClick={onRefresh}>
          刷新
        </Button>
      </div>

      {returnedFromCheckout && !current && (
        <p className="mt-2 text-sm text-kumo-subtle">
          已收到付款结果，套餐通常在一分钟内生效。若仍未更新，请稍后点「刷新」。
        </p>
      )}
      {current && (
        <p className="mt-2 text-sm text-kumo-subtle">
          {current.amount} {current.currency}
          {current.current_period_end
            ? current.status === "canceling"
              ? `，服务持续到 ${current.current_period_end.slice(0, 10)}，之后不再续费`
              : `，当前周期至 ${current.current_period_end.slice(0, 10)}`
            : ""}
          {current.status === "past_due" && "。续费扣款失败，请到付款邮件中更新支付方式。"}
        </p>
      )}

      {!current && (
        <div className="mt-4 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {prices.map((price) => (
            <div key={price.id} className="rounded-lg border border-kumo-line p-4">
              <div className="font-medium">{price.plan_name}</div>
              <div className="mt-2 text-2xl">
                {price.amount}{" "}
                <span className="text-sm text-kumo-subtle">
                  {price.currency} / {price.billing_period === "monthly" ? "月" : "年"}
                </span>
              </div>
              <Button
                className="mt-4 w-full"
                variant="secondary"
                disabled={pending}
                onClick={() => checkout(price.id)}
              >
                订阅
              </Button>
            </div>
          ))}
        </div>
      )}

      {current?.status === "active" && (
        <Button
          className="mt-4"
          variant="secondary-destructive"
          disabled={pending}
          onClick={() => setCancel(true)}
        >
          到期后取消
        </Button>
      )}
      {current?.status === "canceling" && (
        <Button
          className="mt-4"
          variant="secondary"
          disabled={pending}
          onClick={() => setCancel(false)}
        >
          恢复自动续费
        </Button>
      )}
      {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}
    </LayerCard>
  );
}
