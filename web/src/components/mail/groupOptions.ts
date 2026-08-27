import type { MailGroupNode } from "@/api/mail";

// 分组压成下拉项的逻辑，此前在三个地方各写了一份：导入页、账号抽屉、账号筛选栏。
// 三份还各自不一样（缩进按 level 还是按遍历深度），统一收在这里。
//
// 分组现在是平的一层，这里不再有「树」的概念——没有缩进，也没有「含子树的账号数」。

interface GroupSelectOptions {
  /** 传了就在最前面插一项空值，用于「全部分组」这类不限定的选项。 */
  allLabel?: string;
  /** 是否在名称后面缀上账号数。 */
  counts?: boolean;
}

/** groupSelectItems 生成 Kumo `Select` 用的 {label, value} 列表。 */
export function groupSelectItems(
  groups: MailGroupNode[],
  { allLabel, counts }: GroupSelectOptions = {},
): { label: string; value: string }[] {
  const items = allLabel ? [{ label: allLabel, value: "" }] : [];
  for (const group of groups) {
    const suffix = counts ? ` (${group.account_count})` : "";
    items.push({ label: `${group.name}${suffix}`, value: group.id });
  }
  return items;
}

/**
 * findSystemGroup 返回注册时自动建出的系统分组。
 *
 * 它是账号在「没指定分组」时的归宿（后端 `AccountService.resolveGroup`），
 * 也是删除其它分组时账号的回落目标，因此每个租户必然有且只有一个。
 */
export function findSystemGroup(groups: MailGroupNode[]): MailGroupNode | undefined {
  return groups.find((group) => group.is_system);
}
