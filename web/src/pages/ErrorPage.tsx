import { Button, LinkButton } from "@cloudflare/kumo/components/button";
import { Envelope } from "@phosphor-icons/react";
import { Link, isRouteErrorResponse, useRouteError } from "react-router-dom";
import { useAuthStore } from "@/store/authStore";

// 路由级错误边界。没有它时，任何渲染期抛错都会让页面变成一片空白。
//
// 这里承担两种完全不同的情况，措辞必须分开：
//
// - **地址不存在**（404）：页面没坏，是这个地址本来就没有。给技术细节没有意义，
//   显示一段 "No route matches URL ..." 只会让人以为自己碰坏了什么。
// - **渲染期抛错**：这才是真出错。技术细节要留着，用户报障时能截图给出来。
//
// 这个组件挂在 errorElement 上，**不经过 Layout**，所以既没有导航栏也没有顶栏——
// 品牌标记和出口链接都得自己带，否则用户到了这一页就只能按浏览器后退。
export default function ErrorPage() {
  const error = useRouteError();
  const authed = useAuthStore((s) => s.isAuthenticated);
  const notFound = isRouteErrorResponse(error) && error.status === 404;
  const detail = notFound ? "" : error instanceof Error ? error.message : String(error ?? "");
  // 已登录的人回首页没什么用，他要的是回到工作台。
  const home = authed ? "/mail" : "/";

  return (
    <div className="grid min-h-dvh place-items-center bg-kumo-base px-5 py-16">
      <div className="w-full max-w-[440px] text-center">
        <Link to={home} className="mb-8 inline-flex items-center gap-2.5 text-kumo-strong">
          <span className="grid size-7 place-items-center rounded-lg bg-linear-to-br from-ebx-brand-grad-from to-ebx-brand-grad-to text-white">
            <Envelope size={15} weight="fill" />
          </span>
          <span className="text-sm font-semibold tracking-[-0.01em]">Emailbox</span>
        </Link>

        <h1 className="display-md text-kumo-strong">{notFound ? "页面不存在" : "页面出错了"}</h1>
        <p className="mt-3 text-sm text-kumo-subtle">
          {notFound
            ? "这个地址没有对应的页面，可能是链接过期或输错了。"
            : "抱歉，这个页面无法正常显示。"}
        </p>

        {detail && (
          <p className="mt-5 rounded-lg border border-kumo-line bg-kumo-canvas p-3 text-left text-xs break-words text-kumo-subtle">
            {detail}
          </p>
        )}

        <div className="mt-7 flex items-center justify-center gap-3">
          <LinkButton href={home} variant="secondary">
            {authed ? "回到邮箱" : "返回首页"}
          </LinkButton>
          {/* 404 重新加载还是 404，这个按钮只对渲染错误有意义。 */}
          {!notFound && (
            <Button variant="secondary" onClick={() => window.location.reload()}>
              重新加载
            </Button>
          )}
        </div>
      </div>
    </div>
  );
}
