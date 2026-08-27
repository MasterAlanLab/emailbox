import { Badge } from "@cloudflare/kumo/components/badge";
import { Button } from "@cloudflare/kumo/components/button";
import { Checkbox } from "@cloudflare/kumo/components/checkbox";
import { Pagination } from "@cloudflare/kumo/components/pagination";
import { Select } from "@cloudflare/kumo/components/select";
import { PencilSimple, Tray } from "@phosphor-icons/react";
import type { AccountStatus, MailAccount } from "@/api/mail";
import { useSelectionStore } from "@/store/selectionStore";
import { EmptyState } from "./EmptyState";
import { VirtualList } from "./VirtualList";

const STATUS_LABEL: Record<AccountStatus, { label: string; variant: string }> = {
  active: { label: "正常", variant: "green" },
  disabled: { label: "已停用", variant: "neutral" },
  // banned 是协议层识别到「账号被封」时置位的，视觉上要和普通停用区分开
  banned: { label: "已封禁", variant: "red" },
};

// 列宽用 grid 而不是 table：虚拟化要给每行绝对定位，table 布局做不到。
//
// 按**容器**宽度而不是视口宽度切换列数（@container）：这一栏夹在左栏和右栏中间，
// 1440 的视口下它自己只有 570px 左右。此前用的是一组固定列宽，加起来 716px，
// 于是最后一列直接溢出、飘到右栏的邮件列表上面去了。
// 视口断点在这里没有意义——决定放不放得下的是这一栏自己有多宽。
//
// 窄：复选框 / 邮箱 / 状态 / 令牌 / 操作
// 宽（@3xl 起）：再加回 服务商
const COLUMNS_NARROW = "grid-cols-[2.5rem_minmax(0,1fr)_5rem_5rem_2.25rem]";
const COLUMNS_WIDE = "@3xl:grid-cols-[2.5rem_minmax(0,1fr)_7rem_5rem_5rem_2.25rem]";
const GRID = `grid items-center gap-2 ${COLUMNS_NARROW} ${COLUMNS_WIDE}`;
// 次要列：窄的时候整格不渲染，列数才和上面的模板对得上。
const SECONDARY_CELL = "hidden @3xl:flex";
const ROW_HEIGHT = 44;
// 上限对齐后端 model.MaxAccountPageSize。
const PAGE_SIZES = [25, 50, 100, 200];

interface AccountListProps {
  accounts: MailAccount[];
  loading: boolean;
  // activeID 是右侧邮件栏正在看的那个账号，列表里高亮出来，
  // 否则用户滚动几屏之后就不知道右边的邮件属于哪个邮箱了。
  activeID: string | null;
  onSelect: (account: MailAccount) => void;
  onEdit: (account: MailAccount) => void;
  page: number;
  perPage: number;
  total: number;
  onPageChange: (page: number) => void;
  onPerPageChange: (perPage: number) => void;
}

export function AccountList({
  accounts,
  loading,
  activeID,
  onSelect,
  onEdit,
  page,
  perPage,
  total,
  onPageChange,
  onPerPageChange,
}: AccountListProps) {
  const selected = useSelectionStore((s) => s.selected);
  const selectPage = useSelectionStore((s) => s.selectPage);
  const clearPage = useSelectionStore((s) => s.clearPage);

  const pageIDs = accounts.map((a) => a.id);
  const allOnPageSelected = pageIDs.length > 0 && pageIDs.every((id) => selected.has(id));

  if (loading) {
    return <p className="p-6 text-sm text-kumo-subtle">加载中…</p>;
  }

  return (
    // @container + overflow-hidden：前者让列数跟着这一栏自己的宽度走，
    // 后者是兜底——任何列宽算错都只会在本栏内被裁掉，不会再画到相邻面板上。
    <div className="@container flex min-h-0 flex-1 flex-col overflow-hidden">
      <div
        className={`${GRID} shrink-0 border-b border-kumo-line px-3 py-2 text-xs text-kumo-subtle`}
      >
        <Checkbox
          checked={allOnPageSelected}
          onCheckedChange={() => (allOnPageSelected ? clearPage(pageIDs) : selectPage(pageIDs))}
          aria-label="全选当前页"
        />
        <span className="font-medium">邮箱</span>
        <span className={`${SECONDARY_CELL} font-medium`}>服务商</span>
        <span className="font-medium">状态</span>
        <span className="font-medium">令牌</span>
        <span className="sr-only">操作</span>
      </div>

      {accounts.length === 0 ? (
        <EmptyState
          icon={Tray}
          title="这里还没有邮箱账号"
          description="用顶部的「导入邮箱」批量添加，或换一个筛选条件试试。"
        />
      ) : (
        <VirtualList
          items={accounts}
          rowHeight={ROW_HEIGHT}
          className="min-h-0 flex-1"
          renderRow={(account) => (
            <AccountRow
              account={account}
              active={account.id === activeID}
              onSelect={onSelect}
              onEdit={onEdit}
            />
          )}
        />
      )}

      {/* 分页常驻（只要有账号就显示）。后端每页最多 200 条，
          没有这一条的时候，第 51 个账号往后在界面上根本不存在。

          只借用 Kumo 的 Controls（纯图标翻页按钮），信息和每页条数自己写：
          Pagination.Info / PageSize 的文案是写死的英文（"Showing 1-50 of 50"、"Per page"），
          放进一个全中文的界面里很突兀，而 Kumo 没给它们留文案接口。 */}
      {total > 0 && (
        <div className="flex shrink-0 items-center gap-3 overflow-x-auto border-t border-kumo-line px-3 py-2">
          <Pagination page={page} setPage={onPageChange} perPage={perPage} totalCount={total}>
            {/* 信息文本按容器宽度分两档。这一栏被左右夹着，四栏都展开时只有 400 多像素，
                完整的「第 1–50 条，共 200 条」会把整条分页挤到换行。
                窄的时候总数比区间重要——用户先想知道一共多少个。 */}
            <span className="shrink-0 whitespace-nowrap text-xs text-kumo-subtle tabular-nums @2xl:hidden">
              共 {total} 条
            </span>
            <span className="hidden shrink-0 whitespace-nowrap text-xs text-kumo-subtle tabular-nums @2xl:inline">
              第 {(page - 1) * perPage + 1}–{Math.min(page * perPage, total)} 条，共 {total} 条
            </span>
            <Pagination.Separator />
            <label className="flex shrink-0 items-center gap-1.5 text-xs text-kumo-subtle">
              <span className="hidden whitespace-nowrap @2xl:inline">每页</span>
              {/* 选项上限跟后端的 MaxAccountPageSize(200) 对齐：给出更大的值也会被后端截断，
                  界面上却写着「每页 500」，看起来像是丢了数据。 */}
              <Select
                size="sm"
                className="w-18"
                aria-label="每页条数"
                items={PAGE_SIZES.map((n) => ({ label: String(n), value: String(n) }))}
                value={String(perPage)}
                onValueChange={(value: string | null) => onPerPageChange(Number(value) || 50)}
              />
            </label>
            <Pagination.Controls />
          </Pagination>
        </div>
      )}
    </div>
  );
}

function AccountRow({
  account,
  active,
  onSelect,
  onEdit,
}: {
  account: MailAccount;
  active: boolean;
  onSelect: (account: MailAccount) => void;
  onEdit: (account: MailAccount) => void;
}) {
  // 只订阅这一行的选中状态，避免勾选任何一行都让整张列表重渲染。
  const checked = useSelectionStore((s) => s.selected.has(account.id));
  const toggle = useSelectionStore((s) => s.toggle);

  return (
    <div
      className={`${GRID} border-b border-kumo-hairline px-3 text-sm ${
        active ? "bg-kumo-interact" : "hover:bg-kumo-interact"
      }`}
      style={{ height: ROW_HEIGHT }}
    >
      <Checkbox
        checked={checked}
        onCheckedChange={() => toggle(account.id)}
        aria-label={`选择 ${account.email}`}
      />
      <div className="flex min-w-0 items-center">
        {/* 点邮箱名是「看这个邮箱的信」——这是本页最高频的动作，给它最大的点击区。
            改配置走右侧那个铅笔按钮，两者分开，否则想看信的人每次都先撞进编辑抽屉。
            再点一次收起，所以用 aria-expanded 而不是 aria-current 表达状态。 */}
        <button
          type="button"
          className="truncate font-medium text-kumo-link hover:underline"
          aria-expanded={active}
          title={active ? "再点一次关闭收件箱" : `查看 ${account.email} 的邮件`}
          onClick={() => onSelect(account)}
        >
          {account.email}
        </button>
        {account.aliases.length > 0 && (
          <span className="ml-2 shrink-0 text-xs text-kumo-subtle">
            +{account.aliases.length} 别名
          </span>
        )}
      </div>
      <span className={`${SECONDARY_CELL} truncate text-kumo-subtle`}>{account.provider}</span>
      <span>
        <Badge variant={STATUS_LABEL[account.status].variant as never}>
          {STATUS_LABEL[account.status].label}
        </Badge>
      </span>
      <RefreshCell account={account} />
      <Button
        shape="square"
        size="sm"
        variant="ghost"
        icon={PencilSimple}
        aria-label={`编辑 ${account.email}`}
        onClick={() => onEdit(account)}
      />
    </div>
  );
}

function RefreshCell({ account }: { account: MailAccount }) {
  if (account.last_refresh_status === "never") {
    return <span className="text-xs text-kumo-subtle">未刷新</span>;
  }
  if (account.last_refresh_status === "success") {
    return <span className="text-xs text-kumo-success">正常</span>;
  }
  // 失败原因挂在 title 上：用户最需要知道的是「为什么失败」，
  // 而不是再点进详情页找一次。
  return (
    <span className="text-xs text-kumo-danger" title={account.last_refresh_error}>
      失败
    </span>
  );
}
