import { Button } from "@cloudflare/kumo/components/button";
import { ArrowsIn, ArrowsOut, Copy, X } from "@phosphor-icons/react";
import { useCallback, useEffect, useState } from "react";
import {
  mailApi,
  type Message,
  type MessageDetail as Detail,
  type TenantRef,
  mailScopeKey,
} from "@/api/mail";
import { AttachmentList } from "./AttachmentList";
import { MessageBody } from "./MessageBody";
import { refKey, toRef } from "./messageRef";

interface MessageDetailProps {
  tenantID: TenantRef;
  accountID: string;
  // message 是列表行给的摘要。有了它，详情还在路上时头部就能先显示出来，
  // 不至于整栏空白——详情要走一次远端调用，慢的时候是十几秒。
  message: Message;
  onClose: () => void;
}

interface DetailState {
  key: string;
  detail: Detail | null;
  loading: boolean;
  error: string;
}

const loadingState = (key: string): DetailState => ({
  key,
  detail: null,
  loading: true,
  error: "",
});

export function MessageDetail({ tenantID, accountID, message, onClose }: MessageDetailProps) {
  const [fullscreen, setFullscreen] = useState(false);
  const [copied, setCopied] = useState(false);
  const key = `${mailScopeKey(tenantID)}|${accountID}|${refKey(message)}`;
  // 拆成基本类型再进 effect 依赖：直接依赖 message 对象的话，列表刷新后
  // 拿到的是一个内容相同但引用不同的新对象，详情会跟着白拉一次（还要再扣一次配额）。
  const { id: messageID, id_mode: idMode, folder } = toRef(message);

  const [state, setState] = useState(() => loadingState(key));

  // 与 MessageList 同一个套路：换信时在渲染期同步重置，
  // 否则会有一帧把上一封信的正文挂在新主题下面。
  let view = state;
  if (state.key !== key) {
    view = loadingState(key);
    setState(view);
  }

  useEffect(() => {
    let ignore = false;
    void (async () => {
      try {
        const resp = await mailApi.message(tenantID, accountID, messageID, {
          folder,
          id_mode: idMode,
        });
        if (ignore) return;
        setState((prev) =>
          prev.key === key ? { ...prev, detail: resp.data, loading: false } : prev,
        );
      } catch (e) {
        if (ignore) return;
        const error = e instanceof Error ? e.message : "加载失败";
        setState((prev) => (prev.key === key ? { ...prev, error, loading: false } : prev));
      }
    })();
    return () => {
      ignore = true;
    };
  }, [key, tenantID, accountID, messageID, idMode, folder]);

  // 全屏时 Esc 退出。这是全屏阅读唯一的出口，不给的话用户只能去点右上角。
  useEffect(() => {
    if (!fullscreen) return undefined;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setFullscreen(false);
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [fullscreen]);

  const close = useCallback(() => {
    setFullscreen(false);
    onClose();
  }, [onClose]);

  const detail = view.detail;

  const copyBody = useCallback(async () => {
    if (!detail) return;
    try {
      await navigator.clipboard.writeText(plainText(detail.body, detail.body_type));
      setCopied(true);
      window.setTimeout(() => setCopied(false), 2000);
    } catch {
      // 剪贴板在非安全上下文（http 访问的非 localhost）里不可用。
      // 复制失败不该弹错误对话框打断阅读，按钮维持原样即可。
    }
  }, [detail]);

  return (
    <section
      className={
        fullscreen
          ? "fixed inset-0 z-50 flex flex-col bg-kumo-base"
          : // 不画边框：详情已经是右栏 SplitPane 的下段，上边界由分隔条自己提供，
            // 再加一条 border 会变成两条紧挨着的线。
            "flex min-h-0 min-w-0 flex-1 flex-col"
      }
    >
      <header className="flex items-start gap-2 border-b border-kumo-line px-4 py-3">
        <div className="min-w-0 flex-1">
          <h2 className="truncate font-semibold text-kumo-default">
            {message.subject || "(无主题)"}
          </h2>
          <dl className="mt-1 space-y-0.5 text-xs text-kumo-subtle">
            <MetaRow label="发件人" value={message.from} />
            <MetaRow label="收件人" value={detail?.to ?? message.to} />
            <MetaRow label="抄送" value={detail?.cc ?? message.cc} />
            <MetaRow label="时间" value={formatReceivedAt(message.received_at)} />
          </dl>
        </div>
        <Button
          shape="square"
          size="sm"
          variant="ghost"
          icon={fullscreen ? ArrowsIn : ArrowsOut}
          aria-label={fullscreen ? "退出全屏" : "全屏阅读"}
          onClick={() => setFullscreen((v) => !v)}
        />
        <Button
          shape="square"
          size="sm"
          variant="ghost"
          icon={X}
          aria-label="关闭"
          onClick={close}
        />
      </header>

      {/* 操作条。
          参照设计这里还有「回复 / 转发 / 删除 / 标为未读」，我们只放做得到且不会
          制造状态不一致的那些：
          - 回复/转发：没有发信能力，后端只有读取和删除。
          - 删除、标记已读：已经在列表侧（勾选后出现的批量条）里，那条链路会同步更新
            列表的行状态；在详情里再做一份，列表不会跟着变，用户会看到一封「已删除
            但还在列表里」的信。
          - 复制正文：验证码类邮件的高频动作，纯本地操作，没有同步问题。 */}
      {detail && (
        <div className="flex shrink-0 items-center gap-2 border-b border-kumo-line px-4 py-2">
          <Button size="sm" variant="secondary" icon={Copy} onClick={() => void copyBody()}>
            {copied ? "已复制" : "复制正文"}
          </Button>
        </div>
      )}

      {view.loading && <p className="p-6 text-sm text-kumo-subtle">正在加载邮件正文…</p>}
      {view.error && <p className="p-6 text-sm text-kumo-danger">{view.error}</p>}

      {detail && (
        <>
          <AttachmentList tenantID={tenantID} accountID={accountID} message={detail} />
          <MessageBody body={detail.body} bodyType={detail.body_type} />
        </>
      )}
    </section>
  );
}

// MetaRow 在字段为空时整行不渲染：大多数邮件没有抄送，
// 留一行空的「抄送：」只会把有用的信息挤下去。
function MetaRow({ label, value }: { label: string; value: string }) {
  if (!value) return null;
  return (
    <div className="flex gap-2">
      <dt className="shrink-0">{label}</dt>
      <dd className="min-w-0 flex-1 truncate text-kumo-default">{value}</dd>
    </div>
  );
}

// plainText 把邮件正文转成可复制的纯文本。
//
// 用 DOMParser 而不是正则剥标签：正则会把 `<div>a</div><div>b</div>` 拼成 "ab"，
// 而验证码邮件里那串数字常常正好被这样粘到相邻文字上。
// DOMParser 解析出的是一棵**游离文档**，不进当前页面、不加载资源、不执行脚本，
// 所以拿未净化的 body 进来也是安全的——这里只读 textContent，从不插入 DOM。
function plainText(body: string, bodyType: string): string {
  if (bodyType !== "html") return body;
  const text = new DOMParser().parseFromString(body, "text/html").body.textContent ?? "";
  // 上游的 HTML 里有大量为了排版塞进来的空行，原样复制出去很难读。
  return text.replace(/\n{3,}/g, "\n\n").trim();
}

// 详情栏和列表行不同，这里空间够，直接给完整时间——
// 判断一封信是不是刚到的验证码，秒级精度是有意义的。
function formatReceivedAt(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  return date.toLocaleString("zh-CN", { hour12: false });
}
