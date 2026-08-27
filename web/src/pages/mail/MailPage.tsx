import { EnvelopeSimple } from "@phosphor-icons/react";
import { useCallback, useEffect, useMemo, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { jobApi, type RefreshStats } from "@/api/jobs";
import {
  mailApi,
  mailScopeKey,
  type AccountStatus,
  type MailAccount,
  type MailGroupNode,
  type MailScope,
  type Message,
  type RefreshStatus,
  type TenantRef,
} from "@/api/mail";
import { AccountDrawer } from "@/components/mail/AccountDrawer";
import { AccountFilterBar } from "@/components/mail/AccountFilterBar";
import { AccountList } from "@/components/mail/AccountList";
import { EmptyState } from "@/components/mail/EmptyState";
import { ExportDialog } from "@/components/mail/ExportDialog";
import { MailShell } from "@/components/mail/MailShell";
import { MailSidebar } from "@/components/mail/MailSidebar";
import { MailStatusBar } from "@/components/mail/MailStatusBar";
import { MailToolbar } from "@/components/mail/MailToolbar";
import { MessageDetail } from "@/components/mail/MessageDetail";
import { MessageList } from "@/components/mail/MessageList";
import { SplitPane } from "@/components/mail/SplitPane";
import { refKey } from "@/components/mail/messageRef";
import { useSelectionStore } from "@/store/selectionStore";
import { useTenantStore } from "@/store/tenantStore";

interface MailPageProps {
  // scope 由管理员视图传入（/admin/tenants/:id/mail）。不传时看的是自己的工作空间。
  // 整棵组件树对这两种情形无感知——接口是同构的，差别只有请求前缀。
  scope?: MailScope;
}

export default function MailPage({ scope }: MailPageProps = {}) {
  const activeTenantID = useTenantStore((s) => s.activeTenant?.id) ?? "";

  // scope 是调用方每次渲染新建的对象字面量，直接用会让下面那个取数 effect
  // 每渲染一次就重拉一次。按内容记忆化之后，引用只在真正换租户时才变。
  const scopeTenantID = scope?.tenantID;
  const scopeAdmin = scope?.admin;
  const tenantID = useMemo<TenantRef>(
    () => (scopeTenantID ? { tenantID: scopeTenantID, admin: scopeAdmin } : activeTenantID),
    [scopeTenantID, scopeAdmin, activeTenantID],
  );
  const tenantKey = mailScopeKey(tenantID);
  const [groups, setGroups] = useState<MailGroupNode[]>([]);
  const [accounts, setAccounts] = useState<MailAccount[]>([]);
  const [total, setTotal] = useState(0);
  const [groupID, setGroupID] = useState<string | null>(null);
  // 搜索词来自 URL（顶栏那个搜索框写进去的），不在这里另存一份 state：
  // 两个真源迟早会分叉，表现为「地址栏写着 ?q=abc，列表却是全量」。
  const [searchParams] = useSearchParams();
  const query = searchParams.get("q") ?? "";
  const [status, setStatus] = useState<AccountStatus | "">("");
  // 左栏那一段筛的是 refresh_status（登录成功/失败/从未），
  // 和中栏下拉的 status（正常/停用/封禁）是两个维度，可以叠加。
  const [refreshStatus, setRefreshStatus] = useState<RefreshStatus | "">("");
  // stats 在这里取一次，左栏的计数和底部状态条共用，不各拉一遍。
  const [stats, setStats] = useState<RefreshStats | null>(null);
  // 分页。后端每页默认 50、上限 200（model.DefaultAccountPageSize），
  // 而此前前端既不传 page 也没有翻页控件——账号超过 50 个时，
  // 第 51 个往后的账号在界面上根本不存在，顶部却还写着「共 N 个账号」。
  const [page, setPage] = useState(1);
  const [perPage, setPerPage] = useState(50);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState("");
  const clearSelection = useSelectionStore((s) => s.clear);
  // 选择器必须返回稳定引用：zustand v5 走 useSyncExternalStore，
  // 每次调用都新建数组会让 React 认为快照一直在变，直接死循环（React error #185）。
  // 取回 Set 再 useMemo 成数组。
  const selectedSet = useSelectionStore((s) => s.selected);
  const selectedIDs = useMemo(() => Array.from(selectedSet), [selectedSet]);

  // 右边两栏各自跟着一个选择：看哪个邮箱的信、看哪封信。
  // editAccount 是另一条线（改配置），和「看信」互不影响，所以分开存。
  const [activeAccount, setActiveAccount] = useState<MailAccount | null>(null);
  const [openMessage, setOpenMessage] = useState<Message | null>(null);
  const [editAccount, setEditAccount] = useState<MailAccount | null>(null);
  const [exporting, setExporting] = useState(false);

  // reloadToken 让「刷新」按钮和批量操作完成后能重新触发下面的 effect，
  // 而不需要在事件处理器里再写一份取数逻辑。
  const [reloadToken, setReloadToken] = useState(0);
  const reload = useCallback(() => setReloadToken((v) => v + 1), []);

  useEffect(() => {
    if (!tenantID) return undefined;
    // ignore 防止「先发的慢请求后到」把新筛选条件的结果覆盖掉——
    // 用户快速切换分组时这非常容易发生。
    let ignore = false;
    void (async () => {
      try {
        const [groupResp, accountResp, statsResp] = await Promise.all([
          mailApi.groups(tenantID),
          mailApi.accounts(tenantID, {
            group_id: groupID ?? undefined,
            q: query || undefined,
            status: status || undefined,
            refresh_status: refreshStatus || undefined,
            page,
            limit: perPage,
          }),
          // 统计失败不该让整页报错：左栏计数少几个数字，
          // 远不如「账号列表明明拿到了却显示加载失败」严重。
          jobApi.stats(tenantID).catch(() => null),
        ]);
        if (ignore) return;
        const items = accountResp.data.items;
        setGroups(groupResp.data);
        if (statsResp) setStats(statsResp.data);
        setAccounts(items);
        setTotal(accountResp.data.pagination.total);
        // 正在看信的那个账号如果被新的筛选条件排除掉了（或者干脆被删了），
        // 右边两栏必须跟着收起来，否则界面上会出现一个左边根本找不到的邮箱。
        setActiveAccount((prev) => (prev && items.some((a) => a.id === prev.id) ? prev : null));
        setError("");
      } catch (e) {
        if (ignore) return;
        setError(e instanceof Error ? e.message : "加载失败");
      } finally {
        if (!ignore) setLoading(false);
      }
    })();
    return () => {
      ignore = true;
    };
  }, [tenantID, groupID, query, status, refreshStatus, page, perPage, reloadToken]);

  // 换筛选条件要做两件事：清空选中集、回到第 1 页。
  //
  // - 留着上一批选中项，用户在新视图里点「批量删除」会删掉屏幕上根本看不见的账号。
  // - 换了条件还停在第 7 页，看到的多半是一片空白。
  //
  // 放在事件处理器里而不是 effect：effect 里同步 setState 会触发级联渲染
  // （eslint 的 react-hooks/set-state-in-effect 就是拦这个），而且要晚一帧——
  // 那一帧里旧 page 配着新条件，会先白发一次请求再被下一次覆盖。
  const resetPaging = useCallback(() => {
    setPage(1);
    clearSelection();
  }, [clearSelection]);

  const changeGroup = useCallback(
    (value: string | null) => {
      setGroupID(value);
      resetPaging();
    },
    [resetPaging],
  );
  const changeStatus = useCallback(
    (value: AccountStatus | "") => {
      setStatus(value);
      resetPaging();
    },
    [resetPaging],
  );
  const changeRefreshStatus = useCallback(
    (value: RefreshStatus | "") => {
      setRefreshStatus(value);
      resetPaging();
    },
    [resetPaging],
  );

  const selectAccount = useCallback((account: MailAccount) => {
    setActiveAccount(account);
    setOpenMessage(null); // 换邮箱等于换一批信，上一封的详情留着没有意义
  }, []);

  // 批量删除掉的信如果正好是详情栏里那封，详情要一起关掉——
  // 否则用户还能在那儿点附件下载，请求打到一封已经不存在的邮件上。
  const onMessagesRemoved = useCallback((keys: string[]) => {
    setOpenMessage((prev) => (prev && keys.includes(refKey(prev)) ? null : prev));
  }, []);

  if (!tenantKey) {
    return <p className="p-8 text-sm text-kumo-subtle">正在加载…</p>;
  }

  // 移动端一次只显示一栏，靠层级往下走：账号 → 邮件 → 详情。
  // 返回的路径分别是邮件栏的返回按钮和详情栏的关闭按钮。
  const pane = openMessage && activeAccount ? "detail" : activeAccount ? "messages" : "accounts";
  const paneClass = (self: string) => `${pane === self ? "flex" : "hidden"} md:flex`;

  return (
    <MailShell
      toolbar={
        <MailToolbar tenantID={tenantID} onReload={reload} onExport={() => setExporting(true)} />
      }
      status={<MailStatusBar stats={stats} />}
    >
      {/* 左栏只在 ≥1280 时并列显示，再窄就折进筛选栏的下拉里（06 文档 §5.1）。 */}
      <MailSidebar
        tenantID={tenantID}
        groups={groups}
        groupID={groupID}
        onGroupChange={changeGroup}
        refreshStatus={refreshStatus}
        onRefreshStatusChange={changeRefreshStatus}
        stats={stats}
        onGroupsChanged={reload}
      />

      <section className={`${paneClass("accounts")} min-h-0 min-w-0 flex-1 flex-col`}>
        <AccountFilterBar
          groups={groups}
          groupID={groupID}
          onGroupChange={changeGroup}
          status={status}
          onStatusChange={changeStatus}
          total={total}
        />

        {error && <p className="px-4 py-3 text-sm text-kumo-danger">{error}</p>}

        <AccountList
          accounts={accounts}
          loading={loading}
          activeID={activeAccount?.id ?? null}
          onSelect={selectAccount}
          onEdit={setEditAccount}
          page={page}
          perPage={perPage}
          total={total}
          onPageChange={setPage}
          onPerPageChange={setPerPage}
        />
      </section>

      {/* 右栏：邮件列表在上、详情在下，中间可拖。
          之前详情是第四个并列列，三栏一起挤在 1440 宽里，每栏都不够读；
          竖着切之后，列表和正文各自拿到整个右栏的宽度。 */}
      {activeAccount && (
        <div className="flex min-h-0 min-w-0 flex-1 flex-col border-l border-kumo-line">
          <SplitPane
            storageKey="emailbox.mail.split"
            topClassName={paneClass("messages")}
            bottomClassName={paneClass("detail")}
            top={
              <MessageList
                tenantID={tenantID}
                accountID={activeAccount.id}
                activeMessageKey={openMessage ? refKey(openMessage) : null}
                onOpen={setOpenMessage}
                onRemoved={onMessagesRemoved}
                onBack={() => setActiveAccount(null)}
              />
            }
            bottom={
              openMessage ? (
                <MessageDetail
                  tenantID={tenantID}
                  accountID={activeAccount.id}
                  message={openMessage}
                  onClose={() => setOpenMessage(null)}
                />
              ) : (
                <EmptyState icon={EnvelopeSimple} title="选择一封邮件以预览" />
              )
            }
          />
        </div>
      )}

      {exporting && (
        <ExportDialog
          tenantID={tenantID}
          groupID={groupID}
          selectedIDs={selectedIDs}
          onClose={() => setExporting(false)}
        />
      )}

      {editAccount && (
        <AccountDrawer
          tenantID={tenantID}
          account={editAccount}
          groups={groups}
          onClose={() => setEditAccount(null)}
          onSaved={reload}
        />
      )}
    </MailShell>
  );
}
