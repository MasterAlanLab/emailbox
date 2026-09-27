import { Button } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { adminApi, type BillingSettings } from "@/api/admin";
import { useAsyncAction } from "@/lib/useAsyncAction";

interface PaymentSwitchCardProps {
  settings: BillingSettings;
  onChange: (settings: BillingSettings) => void;
}

// 支付入口开关。环境由部署配置决定，这里只显示；打开前缺什么直接列出来，
// 让管理员知道该去配哪个变量，而不是只看到一个点不动的按钮。
export function PaymentSwitchCard({ settings, onChange }: PaymentSwitchCardProps) {
  const { error, pending, run } = useAsyncAction();
  const blocked = !settings.enabled && settings.missing.length > 0;

  const toggle = () =>
    void run(async () => {
      const r = await adminApi.updateBillingSettings({
        enabled: !settings.enabled,
        default_currency: settings.default_currency,
      });
      onChange(r.data);
    });

  return (
    <LayerCard className="mb-6 p-4">
      <div className="flex flex-wrap items-start gap-4">
        <div className="min-w-0 flex-1">
          <h2 className="font-medium text-kumo-strong">
            在线订阅{settings.enabled ? "已开启" : "未开启"}
          </h2>
          <p className="mt-1 text-sm text-kumo-subtle">
            通过 Waffo {settings.environment === "prod" ? "生产" : "测试"}
            环境收款（由部署配置决定）。 关闭后用户看不到购买入口，已有订阅的续费与取消照常处理。
          </p>
          {blocked && (
            <ul className="mt-2 list-disc pl-5 text-sm text-kumo-warning">
              {settings.missing.map((item) => (
                <li key={item}>{item}</li>
              ))}
            </ul>
          )}
        </div>
        <Button
          variant={settings.enabled ? "secondary-destructive" : "secondary"}
          disabled={pending || blocked}
          onClick={toggle}
        >
          {settings.enabled ? "关闭在线订阅" : "开启在线订阅"}
        </Button>
      </div>
      {error && <p className="mt-3 text-sm text-kumo-danger">{error}</p>}
    </LayerCard>
  );
}
