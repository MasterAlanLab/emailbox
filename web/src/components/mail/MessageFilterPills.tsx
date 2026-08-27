import { Tabs } from "@cloudflare/kumo/components/tabs";

// 已读状态筛选。
//
// **这是对已加载的那几页做过滤，不是去服务端筛。** 邮件不落库，列表是一页页从上游
// 实时拉的，而上游列表接口没有「只要未读」这个参数。所以这里的语义是
// 「在我已经取回来的邮件里筛」——计数也按已加载的算，用户才不会以为
// 「未读 3」是整个邮箱只有三封未读。
//
// 参照设计里还有一个「加星」，我们没有：Message 上没有 flagged 字段，
// 上游也没同步过来，做出来只会是一个永远筛不出东西的标签。

export type ReadFilter = "all" | "unread" | "read";

const ITEMS: { value: ReadFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "unread", label: "未读" },
  { value: "read", label: "已读" },
];

interface MessageFilterPillsProps {
  value: ReadFilter;
  onChange: (value: ReadFilter) => void;
  counts: Record<ReadFilter, number>;
}

export function MessageFilterPills({ value, onChange, counts }: MessageFilterPillsProps) {
  return (
    <Tabs
      variant="segmented"
      size="sm"
      value={value}
      onValueChange={(next) => onChange(next as ReadFilter)}
      tabs={ITEMS.map((item) => ({
        value: item.value,
        label: counts[item.value] > 0 ? `${item.label} ${counts[item.value]}` : item.label,
      }))}
    />
  );
}
