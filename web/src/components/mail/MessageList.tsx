import { Button, RefreshButton } from "@cloudflare/kumo/components/button";
import { CaretLeft, Envelope } from "@phosphor-icons/react";
import { useCallback, useEffect, useState } from "react";
import {
  mailApi,
  type MailFolder,
  type Message,
  type MessageRef,
  type TenantRef,
  mailScopeKey,
} from "@/api/mail";
import { EmptyState } from "./EmptyState";
import { FolderTabs } from "./FolderTabs";
import { MessageBatchBar } from "./MessageBatchBar";
import { MessageFilterPills, type ReadFilter } from "./MessageFilterPills";
import { refKey } from "./messageRef";
import { MESSAGE_ROW_HEIGHT, MessageRow } from "./MessageRow";
import { VirtualList } from "./VirtualList";

// 一页 25 条。后端上限是 50（service.maxTop），但首屏越小出得越快，
// 而每一次拉取都要打上游，宁可让用户按需再要一页。
const PAGE_SIZE = 25;

// Page 是「某一批数据」的全部状态，key 标明它属于哪个账号/文件夹/第几次刷新。
// 收拢成一个对象是为了能在渲染期一次性重置——切账号或切文件夹时若用 effect 逐个
// setState，重置会晚一帧发生，那一帧里旧文件夹的邮件仍然挂在新文件夹的标签下，
// 用户此时点批量删除，删的是看不见的信。
interface Page {
  key: string;
  items: Message[];
  channel: string;
  reachedEnd: boolean;
  loading: boolean;
  error: string;
  selected: Set<string>;
}

const emptyPage = (key: string): Page => ({
  key,
  items: [],
  channel: "",
  reachedEnd: false,
  loading: true,
  error: "",
  selected: new Set(),
});

const message = (e: unknown) => (e instanceof Error ? e.message : "加载失败");

interface MessageListProps {
  tenantID: TenantRef;
  accountID: string;
  activeMessageKey: string | null;
  onOpen: (message: Message) => void;
  // 删除后要通知外层：正在看的那封如果被删了，详情栏必须一起关掉。
  onRemoved: (keys: string[]) => void;
  // onBack 只在移动端单栏模式下用得上：那时账号列表是被这一栏盖住的，
  // 没有这个按钮就退不回去。
  onBack: () => void;
}

export function MessageList({
  tenantID,
  accountID,
  activeMessageKey,
  onOpen,
  onRemoved,
  onBack,
}: MessageListProps) {
  const [folder, setFolder] = useState<MailFolder>("inbox");
  const [readFilter, setReadFilter] = useState<ReadFilter>("all");
  const [reloadToken, setReloadToken] = useState(0);
  // 用 mailScopeKey 而不是直接插 tenantID：scope 可能是对象，
  // 插进模板串会变成 "[object Object]"，于是所有租户共用同一个 key，
  // 管理员切到另一个租户时列表不重置——看到的还是上一个人的邮件。
  const key = `${mailScopeKey(tenantID)}|${accountID}|${folder}|${reloadToken}`;

  const [page, setPage] = useState(() => emptyPage(key));

  // 渲染期同步重置（React 官方的 adjusting-state-when-props-change 模式）。
  // 这一版的输出会被 React 丢弃并立刻用新 state 重渲染，所以下面统一用 view 而不是 page。
  let view = page;
  if (page.key !== key) {
    view = emptyPage(key);
    setPage(view);
  }

  // 所有写回都先比对 key：慢请求返回时用户可能已经切走了，
  // 这个判断让过期结果无害地落空（cleanup 的 ignore 只能挡住同一个 effect 实例）。
  const patch = useCallback(
    (k: string, fn: (prev: Page) => Page) => setPage((prev) => (prev.key === k ? fn(prev) : prev)),
    [],
  );

  useEffect(() => {
    let ignore = false;
    void (async () => {
      try {
        const resp = await mailApi.messages(tenantID, accountID, { folder, top: PAGE_SIZE });
        if (ignore) return;
        patch(key, (prev) => ({
          ...prev,
          items: resp.data.items,
          channel: resp.data.channel,
          reachedEnd: resp.data.items.length < PAGE_SIZE,
          loading: false,
        }));
      } catch (e) {
        if (!ignore) patch(key, (prev) => ({ ...prev, error: message(e), loading: false }));
      }
    })();
    return () => {
      ignore = true;
    };
  }, [key, tenantID, accountID, folder, patch]);

  const loadMore = useCallback(
    async (skip: number) => {
      patch(key, (prev) => ({ ...prev, loading: true, error: "" }));
      try {
        const resp = await mailApi.messages(tenantID, accountID, { folder, skip, top: PAGE_SIZE });
        patch(key, (prev) => ({
          ...prev,
          items: [...prev.items, ...resp.data.items],
          reachedEnd: resp.data.items.length < PAGE_SIZE,
          loading: false,
        }));
      } catch (e) {
        patch(key, (prev) => ({ ...prev, error: message(e), loading: false }));
      }
    },
    [key, tenantID, accountID, folder, patch],
  );

  const toggle = useCallback(
    (m: Message) => {
      patch(key, (prev) => {
        const selected = new Set(prev.selected);
        if (!selected.delete(refKey(m))) selected.add(refKey(m));
        return { ...prev, selected };
      });
    },
    [key, patch],
  );

  // 批量操作后按返回结果就地改本地状态，而不是重新拉一次列表：
  // 重拉要再打一次上游、再扣一次配额，用户却只是标了个已读。
  const applyRead = useCallback(
    (done: MessageRef[]) => {
      const keys = new Set(done.map(refKey));
      patch(key, (prev) => ({
        ...prev,
        items: prev.items.map((m) => (keys.has(refKey(m)) ? { ...m, is_read: true } : m)),
        selected: new Set(),
      }));
    },
    [key, patch],
  );

  const applyDelete = useCallback(
    (done: MessageRef[]) => {
      const keys = new Set(done.map(refKey));
      patch(key, (prev) => ({
        ...prev,
        items: prev.items.filter((m) => !keys.has(refKey(m))),
        selected: new Set(),
      }));
      onRemoved([...keys]);
    },
    [key, patch, onRemoved],
  );

  const selectedMessages = view.items.filter((m) => view.selected.has(refKey(m)));

  // 已读筛选在前端做（上游列表接口没有这个参数），因此计数也按已加载的算。
  const counts = {
    all: view.items.length,
    unread: view.items.filter((m) => !m.is_read).length,
    read: view.items.filter((m) => m.is_read).length,
  };
  const visible =
    readFilter === "all"
      ? view.items
      : view.items.filter((m) => (readFilter === "unread" ? !m.is_read : m.is_read));

  return (
    <section className="flex min-h-0 min-w-0 flex-1 flex-col">
      <header className="flex shrink-0 items-center gap-2 border-b border-kumo-line px-3">
        <Button
          className="md:hidden"
          shape="square"
          size="sm"
          variant="ghost"
          icon={CaretLeft}
          aria-label="返回账号列表"
          onClick={onBack}
        />
        <FolderTabs value={folder} onChange={setFolder} disabled={view.loading} />
        <RefreshButton
          className="ml-auto"
          size="sm"
          variant="ghost"
          disabled={view.loading}
          aria-label="重新拉取"
          onClick={() => setReloadToken((v) => v + 1)}
        />
      </header>

      <div className="flex shrink-0 items-center gap-2 border-b border-kumo-line px-3 py-1.5">
        <MessageFilterPills value={readFilter} onChange={setReadFilter} counts={counts} />
      </div>

      <MessageBatchBar
        tenantID={tenantID}
        accountID={accountID}
        messages={selectedMessages}
        onRead={applyRead}
        onDelete={applyDelete}
      />

      {view.error && <p className="px-3 py-2 text-sm text-kumo-danger">{view.error}</p>}

      {view.loading && view.items.length === 0 ? (
        <p className="p-6 text-sm text-kumo-subtle">正在从服务器拉取邮件…</p>
      ) : visible.length === 0 && !view.error ? (
        <EmptyState
          icon={Envelope}
          title={readFilter === "all" ? "这个文件夹里没有邮件" : "没有符合筛选条件的邮件"}
        />
      ) : (
        <VirtualList
          items={visible}
          rowHeight={MESSAGE_ROW_HEIGHT}
          className="min-h-0 flex-1"
          renderRow={(m) => (
            <MessageRow
              message={m}
              active={activeMessageKey === refKey(m)}
              checked={view.selected.has(refKey(m))}
              onOpen={onOpen}
              onToggle={toggle}
            />
          )}
        />
      )}

      {/* 用「加载更多」而不是滚动到底自动加载：每一页都是一次远端调用 +
          一次 daily_mail_fetch 扣减，随手滚两下就把当天配额刷掉说不过去。 */}
      {view.items.length > 0 && !view.reachedEnd && (
        <div className="border-t border-kumo-line p-2">
          <Button
            className="w-full"
            size="sm"
            variant="secondary"
            disabled={view.loading}
            onClick={() => void loadMore(view.items.length)}
          >
            {view.loading ? "加载中…" : "加载更多"}
          </Button>
        </div>
      )}

      {view.channel && (
        <p className="border-t border-kumo-line px-3 py-1 text-xs text-kumo-subtle">
          通道 {view.channel}
        </p>
      )}
    </section>
  );
}
