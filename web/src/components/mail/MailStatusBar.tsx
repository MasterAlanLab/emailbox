import { useEffect, useState } from "react";
import type { RefreshStats } from "@/api/jobs";
import { StatusDot } from "./StatusDot";

// 底部常驻状态条。纯展示，数据由 MailPage 取（和左栏计数是同一份，
// 两处各拉一遍的话，刷新时机不同就会出现左栏和底部数字对不上）。
//
// 数据复用已有的 /mail/refresh/stats——它返回的 total/success/failed/never
// 正好是「账号总数 / 登录成功 / 登录失败 / 尚未登录」，三条聚合 SQL，很便宜。
// 为此再开一个 /mail/stats 端点是重复造一遍。
//
// 参照设计里还有一个「全局未读数」，这里**没有**：邮件不落库，是每次实时从上游拉的，
// 要统计全租户未读就得为每个账号打一次上游请求（还各扣一次 daily_mail_fetch 配额）。
// 未读数只在打开某个账号时才有意义，因此放在邮件列表那一栏里显示。

interface MailStatusBarProps {
  stats: RefreshStats | null;
}

export function MailStatusBar({ stats }: MailStatusBarProps) {
  return (
    <div className="flex h-(--ebx-status-h) shrink-0 items-center gap-4 overflow-x-auto border-t border-kumo-line bg-kumo-canvas px-4 text-xs text-kumo-subtle">
      <span className="shrink-0 tabular-nums">{stats?.total ?? 0} 账号</span>
      <Stat tone="online" label="登录成功" value={stats?.success} />
      <Stat tone="error" label="登录失败" value={stats?.failed} />
      <Stat tone="idle" label="尚未登录" value={stats?.never} />

      <div className="ml-auto flex shrink-0 items-center gap-4">
        <span>邮箱助手</span>
        <Clock />
      </div>
    </div>
  );
}

function Stat({
  tone,
  label,
  value,
}: {
  tone: "online" | "error" | "idle";
  label: string;
  value: number | undefined;
}) {
  return (
    <span className="flex shrink-0 items-center gap-1.5 tabular-nums">
      <StatusDot tone={tone} />
      {label} {value ?? 0}
    </span>
  );
}

// 时钟单独成组件，是为了把每秒一次的重渲染关在这一个叶子节点里。
// 放在 MailStatusBar 里会让整条状态条（连带它的统计数字）每秒重渲染一次；
// 再往上放到 MailPage，就是整棵组件树每秒重来一遍。
function Clock() {
  const [now, setNow] = useState(() => new Date());

  useEffect(() => {
    const id = setInterval(() => setNow(new Date()), 1000);
    return () => clearInterval(id);
  }, []);

  return <time className="tabular-nums">{format(now)}</time>;
}

function format(d: Date) {
  const p = (n: number) => String(n).padStart(2, "0");
  // 手写而不用 toLocaleString：后者的输出随浏览器区域设置变化，
  // 状态条宽度会跟着抖，而且不同用户看到的格式不一样。
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}
