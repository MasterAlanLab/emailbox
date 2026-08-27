import type { Message, MessageRef } from "@/api/mail";

// refKey 是邮件在选中集与「当前打开的是哪封」里的键。
// **不能只用 id**：IMAP 的 UID 是每个文件夹各自编号的，folder=all 视图里
// 收件箱和垃圾箱完全可能撞上同一个 UID，只用 id 会连带选中／高亮另一封信。
//
// 分隔符用 U+0000：它不可能出现在文件夹名或邮件 id 里，因此不存在
// 「folder=a, id=b|c」与「folder=a|b, id=c」撞键的可能。写成转义序列而不是
// 裸控制字符，否则 git 与 grep 会把整个源文件当二进制处理。
export const refKey = (m: Message | MessageRef) => `${m.folder}\u0000${m.id}`;

// toRef 取的是**这封信自己的** folder / id_mode，而不是当前标签页的。
// folder=all 是服务层归并出来的视图，详情、附件、批量接口都不接受 all，
// 必须回填每封信真实所在的文件夹（见 pkg/service/message_service.go 的 ValidFolder 判断）。
export const toRef = (m: Message): MessageRef => ({
  id: m.id,
  id_mode: m.id_mode,
  folder: m.folder,
});
