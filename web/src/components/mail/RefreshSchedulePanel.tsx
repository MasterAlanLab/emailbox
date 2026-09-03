import { LayerCard } from "@cloudflare/kumo/components/layer-card";
import { Select } from "@cloudflare/kumo/components/select";
import { useState } from "react";
import { mailApi, type MailGroupNode, type TenantRef } from "@/api/mail";

// 可选的间隔。取值必须落在后端 model.ValidRefreshIntervalMinutes 的区间里
// （0 或 10080~43200 分钟，即 7~30 天），否则保存会被打回。
//
// 只给这几档而不是让用户填分钟数：这个值没有精调的意义。refresh_token 是滑动过期
// （连续 90 天不用才失效），定时刷新要解决的只是「别让它因为长期没人碰而作废」，
// 每周一次已经绰绰有余，7 天和 8 天不会有任何可观察的差别。
// 而一个自由输入框反而会诱使用户去填 5 分钟，然后被服务商风控。
const INTERVALS: { label: string; value: string }[] = [
  { label: "关闭", value: "0" },
  { label: "每 7 天", value: "10080" },
  { label: "每 14 天", value: "20160" },
  { label: "每 30 天", value: "43200" },
];

interface RefreshSchedulePanelProps {
  tenant: TenantRef;
  groups: MailGroupNode[];
  /** 保存成功后回调，用于把 next_refresh_at 的新值取回来。 */
  onSaved: () => void;
}

export function RefreshSchedulePanel({ tenant, groups, onSaved }: RefreshSchedulePanelProps) {
  // 按分组 id 记录正在保存 / 保存失败的状态。整块面板共用一个 pending 会让
  // 改 A 分组时 B 分组的下拉也变灰，而它们之间没有任何关系。
  const [saving, setSaving] = useState<Record<string, boolean>>({});
  const [errors, setErrors] = useState<Record<string, string>>({});

  async function save(groupID: string, minutes: number) {
    setSaving((s) => ({ ...s, [groupID]: true }));
    setErrors((e) => ({ ...e, [groupID]: "" }));
    try {
      await mailApi.updateGroup(tenant, groupID, { refresh_interval_minutes: minutes });
      onSaved();
    } catch (e) {
      setErrors((prev) => ({
        ...prev,
        [groupID]: e instanceof Error ? e.message : "保存失败",
      }));
    } finally {
      setSaving((s) => ({ ...s, [groupID]: false }));
    }
  }

  if (groups.length === 0) return null;

  return (
    <LayerCard className="mb-6 p-4">
      <h2 className="text-sm font-medium text-kumo-strong">定时刷新</h2>
      <p className="mt-1 text-xs text-kumo-subtle">
        每个分组各自计时，到点自动提交一次该分组的刷新任务。同一时刻只会有一个刷新任务在跑，
        其余分组顺延到它结束之后。
      </p>

      <div className="mt-4 flex flex-col">
        {groups.map((group) => (
          <div
            key={group.id}
            className="flex flex-wrap items-center gap-x-4 gap-y-2 border-b border-kumo-hairline py-3 last:border-b-0"
          >
            <div className="min-w-0 flex-1">
              <p className="truncate text-sm text-kumo-strong">{group.name}</p>
              <p className="text-xs text-kumo-subtle">{group.account_count} 个账号</p>
            </div>

            <Select
              className="w-36"
              aria-label={`${group.name} 的刷新间隔`}
              items={INTERVALS}
              // Select 的 value 是字符串，而间隔在模型里是数字，两边要显式转换。
              value={String(group.refresh_interval_minutes)}
              disabled={saving[group.id]}
              onValueChange={(value: string | null) => {
                if (value === null) return;
                void save(group.id, Number(value));
              }}
            />

            <div className="w-44 text-xs text-kumo-subtle">
              {saving[group.id]
                ? "保存中…"
                : group.refresh_interval_minutes > 0
                  ? `下次 ${formatNextRefresh(group.next_refresh_at)}`
                  : "未开启"}
            </div>

            {errors[group.id] && (
              <p className="w-full text-xs text-kumo-danger">{errors[group.id]}</p>
            )}
          </div>
        ))}
      </div>

      <p className="mt-3 text-xs text-kumo-subtle">
        分组里没有 refresh_token 的账号会被跳过；整组都没有时不会产生任务。
      </p>
    </LayerCard>
  );
}

// 下次刷新时刻。后端存的是 UTC，这里按浏览器本地时区显示——
// 用户看到的「今晚 3 点」必须是他自己的 3 点。
function formatNextRefresh(at: string | null): string {
  if (!at) return "待定";
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return "待定";
  return date.toLocaleString("zh-CN", { hour12: false });
}
