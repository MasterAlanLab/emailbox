import { LinkButton } from "@cloudflare/kumo/components/button";
import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { ArrowSquareOut } from "@phosphor-icons/react";
import { PageShell } from "@/components/layout/PageShell";

interface Resource {
  name: string;
  description: string;
  href: string;
  affiliate?: boolean;
}

interface ResourceGroup {
  title: string;
  description: string;
  items: Resource[];
}

const GROUPS: ResourceGroup[] = [
  {
    title: "网络与部署",
    description: "为邮箱分组准备代理出口，或部署自己的 Emailbox 实例。",
    items: [
      {
        name: "Free Proxy",
        description: "在自己的 VPS 上运行免费代理池，可接入 Emailbox 的分组代理。",
        href: "https://github.com/MasterAlanLab/free-proxy",
      },
      {
        name: "搬瓦工",
        description: "提供海外 VPS 与三网优化线路，支持支付宝。",
        href: "https://cutt.ly/qywJNWzd",
        affiliate: true,
      },
      {
        name: "DMIT",
        description: "提供海外 VPS 与三网优化线路，支持支付宝。",
        href: "https://cutt.ly/YywJIzY0",
        affiliate: true,
      },
    ],
  },
  {
    title: "批量业务工具",
    description: "批量账号场景中常用的辅助服务与工具。",
    items: [
      {
        name: "海外账号、电话卡",
        description: "TG、TikTok 等海外平台的账号与电话卡资源。",
        href: "https://cutt.ly/dywt86NC",
        affiliate: true,
      },
      {
        name: "Captcha.run",
        description: "用于自动化流程中的验证码识别。",
        href: "https://captcha.run/sso?inviter=542f4f4f-31b6-4b70-b485-c4762c45d1e8",
        affiliate: true,
      },
      {
        name: "YesCaptcha",
        description: "价格较低的验证码识别服务。",
        href: "https://cutt.ly/Mywt39r0",
        affiliate: true,
      },
      {
        name: "比特指纹浏览器",
        description: "适合需要隔离浏览器环境的批量账号场景。",
        href: "https://client.bitbrowser.cn/register?lang=zh&code=Alan123",
        affiliate: true,
      },
      {
        name: "海外虚拟信用卡",
        description: "用于支付支持范围有限的海外服务。",
        href: "https://cutt.ly/IyrMR4Mg",
        affiliate: true,
      },
    ],
  },
  {
    title: "其他服务",
    description: "内容检索、AI 服务与订阅相关资源。",
    items: [
      {
        name: "Telegram 资源搜索机器人",
        description: "在 Telegram 内搜索频道、群组与相关资源。",
        href: "https://cutt.ly/2yeh3GOE",
        affiliate: true,
      },
      {
        name: "满血 CC / GPT 中转",
        description: "提供 GPT 等 AI 服务的中转访问。",
        href: "https://cutt.ly/JywJG3G5",
        affiliate: true,
      },
      {
        name: "订阅合租拼车",
        description: "影视会员与 AI 订阅的合租服务。",
        href: "https://cutt.ly/5ywt8vb4",
        affiliate: true,
      },
    ],
  },
];

export default function ResourcesPage() {
  return (
    <PageShell title="资源推荐" description="批量邮箱管理、自动化与部署过程中可能用到的服务。">
      <p className="mb-8 max-w-3xl text-sm leading-6 text-kumo-subtle">
        其中标记为「推广」的链接可能为作者带来少量返佣，不会额外增加你的费用。请根据自己的实际需求选择。
      </p>

      <div className="space-y-10">
        {GROUPS.map((group) => (
          <section key={group.title}>
            <h2 className="text-lg font-medium text-kumo-strong">{group.title}</h2>
            <p className="mt-1 text-sm text-kumo-subtle">{group.description}</p>

            <div className="mt-4 grid gap-3 md:grid-cols-2 xl:grid-cols-3">
              {group.items.map((resource) => (
                <LayerCard key={resource.name} className="flex min-h-44 flex-col p-5">
                  <div className="flex items-start justify-between gap-3">
                    <h3 className="font-medium text-kumo-strong">{resource.name}</h3>
                    {resource.affiliate && (
                      <span className="shrink-0 text-xs text-kumo-subtle">推广</span>
                    )}
                  </div>
                  <p className="mt-3 flex-1 text-sm leading-6 text-kumo-default">
                    {resource.description}
                  </p>
                  <div className="mt-5">
                    <LinkButton
                      href={resource.href}
                      external
                      rel="noopener noreferrer sponsored nofollow"
                      variant="secondary"
                    >
                      访问网站
                      <ArrowSquareOut size={14} />
                    </LinkButton>
                  </div>
                </LayerCard>
              ))}
            </div>
          </section>
        ))}
      </div>
    </PageShell>
  );
}
