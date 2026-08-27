import { DownloadSimple, FileArrowDown } from "@phosphor-icons/react";
import { mailApi, type AttachmentMeta, type MessageDetail, type TenantRef } from "@/api/mail";

interface AttachmentListProps {
  tenantID: TenantRef;
  accountID: string;
  message: MessageDetail;
}

export function AttachmentList({ tenantID, accountID, message }: AttachmentListProps) {
  // 内嵌图片（cid: 引用）不单独列出来：它们已经在正文里显示了，
  // 列出来只会让「这封信有 8 个附件」这种提示失去意义。
  //
  // `?? []` 不是多余的防御：Go 的 nil slice 会序列化成 null，而 TS 类型声明的是
  // 非空数组，类型检查因此拦不住。后端已在 MessageService.Detail 里补齐空数组，
  // 这里留一层是因为崩的代价太大——整棵 React 树会被 errorElement 接管，
  // 用户失去的是整个页面而不只是附件区。
  const attachments = (message.attachments ?? []).filter((a) => !a.is_inline);
  if (attachments.length === 0) return null;

  const params = { folder: message.folder, id_mode: message.id_mode };

  return (
    <div className="border-b border-kumo-line px-4 py-3">
      <div className="mb-2 flex items-center gap-3">
        <span className="text-xs font-medium tracking-wide text-kumo-subtle uppercase">
          附件 {attachments.length}
        </span>
        {attachments.length > 1 && (
          <a
            className="inline-flex items-center gap-1 text-xs text-kumo-link hover:underline"
            href={mailApi.attachmentsZipURL(tenantID, accountID, message.id, params)}
          >
            <FileArrowDown size={14} />
            打包下载
          </a>
        )}
      </div>
      <ul className="flex flex-wrap gap-2">
        {attachments.map((attachment) => (
          <li key={attachment.id}>
            {/* 用 <a download> 而不是 XHR + Blob：后端已经带了 Content-Disposition，
                交给浏览器直接下载能省掉一次内存里的完整拷贝，大附件上差别明显。 */}
            <a
              className="flex max-w-64 items-center gap-2 rounded-md border border-kumo-line px-2 py-1 text-sm hover:bg-kumo-tint"
              href={mailApi.attachmentURL(tenantID, accountID, message.id, attachment.id, params)}
              download={attachment.name}
            >
              <DownloadSimple className="shrink-0 text-kumo-subtle" size={14} />
              <span className="min-w-0 flex-1 truncate">{attachment.name || "(未命名)"}</span>
              <span className="shrink-0 text-xs text-kumo-subtle">
                {formatSize(attachment.size)}
              </span>
            </a>
          </li>
        ))}
      </ul>
    </div>
  );
}

function formatSize(bytes: AttachmentMeta["size"]): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(0)} KB`;
  return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
}
