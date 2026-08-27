// 邮箱工作台的外壳骨架：纵向三段——工具条 / 主体 / 状态栏。
//
// 只管纵向分段和滚动边界，不管主体内部怎么分栏（那是 MailPage 的事）。
// 拆出来是因为 /mail 和 /admin/tenants/:id/mail 共用同一套外壳，
// 而这里每一个 min-h-0 都是踩过的坑，只想维护一份。

interface MailShellProps {
  toolbar?: React.ReactNode;
  status?: React.ReactNode;
  children: React.ReactNode;
}

export function MailShell({ toolbar, status, children }: MailShellProps) {
  return (
    <div className="flex min-h-0 flex-1 flex-col">
      {toolbar}
      {/* 主体是唯一会被压缩的一段：工具条和状态栏都是固定高度，
          剩下的高度全归它。min-h-0 + overflow-hidden 把滚动责任
          明确地推给再下一层的每个面板——少了这两个类，
          内部的虚拟列表会把整个外壳顶破（09 文档 §7.2）。 */}
      <div className="flex min-h-0 flex-1 overflow-hidden">{children}</div>
      {status}
    </div>
  );
}
