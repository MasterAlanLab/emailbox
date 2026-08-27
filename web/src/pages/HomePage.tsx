import { LinkButton } from "@cloudflare/kumo/components/button";
import { ArrowRight } from "@phosphor-icons/react";
import { useAuthStore } from "@/store/authStore";

// 首页只有一屏：标语 + 一句说明 + 一个入口，再往下就是页脚。
//
// 原先还有「能力」四张卡、「流程」三步、收尾 CTA 三段。删掉不是嫌它们做得不好，
// 是这些内容对着谁说都不成立：这是个自部署的工具，会打开首页的人要么是自己，
// 要么是被直接给了地址的人——两种人都不需要被再推销一遍产品做什么。
// 一屏放不下的落地页是给冷流量看的，我们没有冷流量。
//
// 剩下这一屏靠排版而不是装饰：display 级标题配激进负字距（-0.04em，
// 见 style.css 的 .display-xl），hero 直接 flex-1 撑满顶栏与页脚之间竖直居中，
// 空白本身就是排版的一部分。
//
// CTA 和站内其它按钮长得一模一样（白底描边，见 AGENTS.md §6.2）。
// 它靠 size="lg" 和四周的空白站出来，不靠填一块颜色——
// 全站只有一种按钮长相，首页也不例外。
export default function HomePage() {
  // 已登录的人看到「免费开始」没有意义，他要的是回到工作台。
  const authed = useAuthStore((state) => state.isAuthenticated);

  return (
    <section className="shell flex flex-1 flex-col justify-center py-20 sm:py-28">
      <h1 className="display-xl max-w-3xl text-kumo-strong text-balance">少折腾，多产出</h1>

      <p className="mt-7 max-w-xl text-lg leading-relaxed text-kumo-default">
        极简、稳定，专为多邮箱管理而生
      </p>

      {/* 只留一个入口。「登录」在右上角的顶栏里已经有了，
          同一个动作在一屏里出现两次，用户得先分辨它们是不是同一件事。 */}
      <div className="mt-10">
        <LinkButton href={authed ? "/mail" : "/register"} variant="secondary" size="lg">
          {authed ? "进入邮箱" : "免费开始"}
          <ArrowRight size={14} />
        </LinkButton>
      </div>
    </section>
  );
}
