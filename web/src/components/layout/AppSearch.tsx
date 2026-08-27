import { Input } from "@cloudflare/kumo/components/input";
import { MagnifyingGlass } from "@phosphor-icons/react";
import { useEffect, useState } from "react";
import { useSearchParams } from "react-router-dom";

// 顶栏的全局搜索。搜的是**邮箱账号**（地址与备注），走服务端的 q 参数。
//
// 参照设计的 placeholder 还写了「邮件主题、发件人」，我们没有：邮件不落库、
// 也没有跨账号的搜索接口，要支持就得为每个账号打一次上游。placeholder 只写做得到的，
// 否则用户会搜一个主题、得到空结果，然后以为是邮件丢了。
//
// 状态放在 URL 而不是某个 store 里：搜索结果因此可以直接分享和前进后退，
// 也免去了「顶栏组件」和「页面组件」之间再拉一根全局状态的线。

export function AppSearch() {
  const [params, setParams] = useSearchParams();
  // 输入框自己是真源，URL 是它的输出。初值从 URL 读一次，
  // 这样带 ?q= 的链接打开时，框里能显示出搜的是什么。
  const [text, setText] = useState(() => params.get("q") ?? "");

  const urlQuery = params.get("q") ?? "";
  useEffect(() => {
    if (text === urlQuery) return undefined;
    // 防抖。每敲一个字母都写一次 URL，等于每个字母都发一次请求，
    // 而账号搜索是打数据库的。300ms 是「打完一个词」的量级。
    const id = window.setTimeout(() => {
      setParams(
        (prev) => {
          const next = new URLSearchParams(prev);
          if (text) next.set("q", text);
          else next.delete("q");
          return next;
        },
        // replace：搜索过程中的每一次击键都留一条历史记录的话，
        // 用户要按十几次后退才能退出这个页面。
        { replace: true },
      );
    }, 300);
    return () => window.clearTimeout(id);
  }, [text, urlQuery, setParams]);

  return (
    <div className="relative min-w-0 flex-1">
      <MagnifyingGlass
        size={16}
        className="pointer-events-none absolute top-1/2 left-3 -translate-y-1/2 text-kumo-subtle"
        aria-hidden
      />
      <Input
        className="w-full pl-9"
        type="search"
        aria-label="搜索邮箱账号"
        placeholder="搜索邮箱账号、备注…"
        value={text}
        onChange={(event) => setText(event.target.value)}
      />
    </div>
  );
}
