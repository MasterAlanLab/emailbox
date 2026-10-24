import { LinkButton } from "@cloudflare/kumo/components/button";
import { ArrowRight } from "@phosphor-icons/react";
import { useAuthStore } from "@/store/authStore";

// 访客在注册前可以先查看服务范围与当前收费；价格和额度集中在定价页，
// 避免管理员调整套餐后，首页残留另一份过期数字。
export default function HomePage() {
  // 已登录的人看到「免费开始」没有意义，他要的是回到工作台。
  const authed = useAuthStore((state) => state.isAuthenticated);

  return (
    <section className="shell flex flex-1 flex-col justify-center py-20 sm:py-28">
      <h1 className="display-xl max-w-3xl text-kumo-strong text-balance">
        第三方邮箱集中托管与批量运维
      </h1>

      <p className="mt-7 max-w-xl text-lg leading-relaxed text-kumo-default">
        支持 Outlook OAuth、Gmail 与主流 IMAP 邮箱批量托管。提供定时令牌轮换、独立代理路由与只读 API
        接入。
      </p>

      <div className="mt-10 flex flex-wrap gap-3">
        <LinkButton href={authed ? "/mail" : "/register"} variant="secondary" size="lg">
          {authed ? "进入控制台" : "免费注册体验"}
          <ArrowRight size={14} />
        </LinkButton>
        <LinkButton href="/pricing" variant="secondary" size="lg">
          查看价格与额度
        </LinkButton>
      </div>
    </section>
  );
}
