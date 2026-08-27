import { Tabs } from "@cloudflare/kumo/components/tabs";
import type { MailFolder } from "@/api/mail";

// 文件夹取值用 Graph 的命名，IMAP 侧由 pkg/mailer/imapx/folders.go 映射到各服务商的实际名称，
// 前端不需要知道那套映射。all 是服务层合成的（收件箱 + 垃圾箱归并），不对应真实文件夹。
const FOLDERS: { value: MailFolder; label: string }[] = [
  { value: "inbox", label: "收件箱" },
  { value: "junkemail", label: "垃圾箱" },
  { value: "deleteditems", label: "已删除" },
  { value: "all", label: "全部" },
];

interface FolderTabsProps {
  value: MailFolder;
  onChange: (folder: MailFolder) => void;
  disabled?: boolean;
}

export function FolderTabs({ value, onChange, disabled }: FolderTabsProps) {
  return (
    <Tabs
      // underline 而不是默认的 segmented：文件夹是这一栏的主导航，
      // 下划线式更像「章节标签」；segmented 那种药丸底色留给下面的已读筛选，
      // 两者用不同的形状，才不会看起来像两排平级的按钮。
      variant="underline"
      size="sm"
      tabs={FOLDERS}
      value={value}
      // 拉取期间锁住切换：每次切换都是一次远端调用，也各扣一次 daily_mail_fetch 配额。
      // 连点四个标签等于扣四次，而用户最终只会看到最后一个的结果。
      onValueChange={(next) => {
        if (!disabled) onChange(next as MailFolder);
      }}
      className={disabled ? "pointer-events-none opacity-60" : undefined}
    />
  );
}
