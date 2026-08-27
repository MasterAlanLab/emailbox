package imapx

import (
	"context"
	"strconv"
	"strings"

	"emailbox/pkg/mailer"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// previewLimit 是列表页摘要的字数上限。
const previewLimit = 200

// selectFolder 挑出并 SELECT 目标邮件夹。
//
// 先试服务商候选表，全失败再 LIST 出所有邮箱按别名打分重试。
// 两轮都失败时报 folder_unavailable，并把试过哪些名字带在 Detail 里——
// 没有这个诊断信息，「垃圾箱打不开」这类工单根本无从下手。
func (c *Client) selectFolder(s *session, cred mailer.Credential, folder mailer.Folder) (uint32, error) {
	if folder == mailer.FolderAll {
		return 0, newError(c.cfg.Channel, mailer.ErrKindProviderError,
			"folder=all 需要由上层拆分后再调用协议层", nil)
	}
	provider := providerCode(cred)
	tried := make([]string, 0, 8)

	for _, name := range FolderCandidates(provider, folder) {
		if data, err := s.imap.Select(name, nil).Wait(); err == nil {
			return data.NumMessages, nil
		}
		tried = append(tried, name)
	}

	// 候选表没命中：服务商可能改了名字，或者用户把邮箱语言设成了别的。
	mailboxes, listErr := listMailboxes(s.imap)
	if listErr != nil {
		e := errorf(c.cfg.Channel, mailer.ErrKindFolderUnavailable, listErr,
			"找不到%s，且无法列出邮箱列表", folderLabel(folder))
		e.Detail = "已试过: " + strings.Join(tried, ", ")
		return 0, e
	}
	for _, match := range MatchFolders(folder, mailboxes) {
		if data, err := s.imap.Select(match.Name, nil).Wait(); err == nil {
			return data.NumMessages, nil
		}
		tried = append(tried, match.Name)
	}

	e := newError(c.cfg.Channel, mailer.ErrKindFolderUnavailable,
		"找不到"+folderLabel(folder), nil)
	e.Detail = "已试过: " + strings.Join(tried, ", ") +
		"；服务器上的邮箱: " + strings.Join(mailboxes, ", ")
	return 0, e
}

func folderLabel(f mailer.Folder) string {
	switch f {
	case mailer.FolderInbox:
		return "收件箱"
	case mailer.FolderJunk:
		return "垃圾箱"
	case mailer.FolderDeleted:
		return "已删除邮件"
	default:
		return string(f)
	}
}

func providerCode(cred mailer.Credential) string {
	if p := strings.TrimSpace(cred.Provider); p != "" {
		return strings.ToLower(p)
	}
	return mailer.ProviderForEmail(cred.Email).Code
}

func listMailboxes(client *imapclient.Client) ([]string, error) {
	entries, err := client.List("", "*", nil).Collect()
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, e.Mailbox)
	}
	return out, nil
}

// List 实现 mailer.Client。
//
// IMAP 没有原生分页。这里不走 Python 的 SEARCH ALL（邮件多时会拉回巨大的 ID 列表），
// 而是用 SELECT 拿到的 EXISTS 计数直接算序号区间——邮箱有十万封信时这是数量级的差别。
func (c *Client) List(
	ctx context.Context, cred mailer.Credential, opt mailer.ListOptions,
) ([]mailer.Message, error) {
	top := opt.Top
	if top <= 0 {
		top = 20
	}
	skip := max(opt.Skip, 0)

	return withSession(ctx, c, cred, func(_ context.Context, s *session) ([]mailer.Message, error) {
		total, err := c.selectFolder(s, cred, opt.Folder)
		if err != nil {
			return nil, err
		}
		if total == 0 {
			return []mailer.Message{}, nil
		}

		// IMAP 的序号是「从旧到新」的 1..total，而列表要按时间倒序，
		// 所以从尾部往前切。skip 超过总数时是空页，不是错误。
		start, end, ok := pageRange(total, skip, top)
		if !ok {
			return []mailer.Message{}, nil
		}

		var seqSet imap.SeqSet
		seqSet.AddRange(start, end)
		fetchOpt := &imap.FetchOptions{
			UID:           true,
			Envelope:      true,
			Flags:         true,
			InternalDate:  true,
			RFC822Size:    true,
			BodyStructure: &imap.FetchItemBodyStructure{},
		}
		buffers, err := s.imap.Fetch(seqSet, fetchOpt).Collect()
		if err != nil {
			return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "抓取邮件列表失败")
		}

		out := make([]mailer.Message, 0, len(buffers))
		for _, buf := range buffers {
			out = append(out, bufferToMessage(buf, opt.Folder, s.idMode()))
		}
		// FETCH 按序号升序返回（旧→新），倒过来给前端。
		reverse(out)
		return out, nil
	})
}

// pageRange 把「跳过 skip 条、取 top 条（按时间倒序）」换算成 IMAP 的序号区间。
func pageRange(total uint32, skip, top int) (start, end uint32, ok bool) {
	if uint32(skip) >= total {
		return 0, 0, false
	}
	// end 是本页最新的一条：从最后一条往前数 skip 条。
	end = total - uint32(skip)
	span := uint32(top)
	if span > end {
		span = end
	}
	start = end - span + 1
	return start, end, true
}

func reverse[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func bufferToMessage(buf *imapclient.FetchMessageBuffer, folder mailer.Folder, idMode string) mailer.Message {
	msg := mailer.Message{
		ID:     messageID(buf, idMode),
		IDMode: idMode,
		Folder: folder,
	}
	if env := buf.Envelope; env != nil {
		msg.Subject = DecodeHeader(env.Subject)
		msg.From = joinAddresses(env.From)
		msg.To = joinAddresses(env.To)
		msg.Cc = joinAddresses(env.Cc)
		msg.ReceivedAt = env.Date
	}
	if !buf.InternalDate.IsZero() {
		// INTERNALDATE 是服务器收到的时间，比发件人自报的 Date 可信
		// （伪造的 Date 会让邮件排到列表最顶上）。
		msg.ReceivedAt = buf.InternalDate
	}
	for _, flag := range buf.Flags {
		if flag == imap.FlagSeen {
			msg.IsRead = true
		}
	}
	msg.HasAttachments = hasAttachments(buf.BodyStructure)
	return msg
}

// messageID 按 idMode 返回该用哪个编号。
//
// UID 与序列号是两套编号，混用会取到错误的邮件——outlookEmail 修过这个真实 bug。
// 列表里标的 IDMode 必须与这里返回的编号一致，详情/附件请求再原样带回来。
func messageID(buf *imapclient.FetchMessageBuffer, idMode string) string {
	if idMode == mailer.IDModeUID && buf.UID != 0 {
		return strconv.FormatUint(uint64(buf.UID), 10)
	}
	return strconv.FormatUint(uint64(buf.SeqNum), 10)
}

func joinAddresses(addrs []imap.Address) string {
	if len(addrs) == 0 {
		return ""
	}
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		addr := a.Addr()
		name := DecodeHeader(a.Name)
		switch {
		case addr == "":
			if name != "" {
				parts = append(parts, name)
			}
		case name == "" || name == addr:
			parts = append(parts, addr)
		default:
			parts = append(parts, name+" <"+addr+">")
		}
	}
	return strings.Join(parts, ", ")
}

func hasAttachments(bs imap.BodyStructure) bool {
	if bs == nil {
		return false
	}
	found := false
	bs.Walk(func(_ []int, part imap.BodyStructure) bool {
		if found {
			return false
		}
		single, ok := part.(*imap.BodyStructureSinglePart)
		if !ok {
			return true
		}
		if single.Disposition() != nil &&
			strings.EqualFold(single.Disposition().Value, "attachment") {
			found = true
			return false
		}
		if single.Filename() != "" {
			found = true
			return false
		}
		return true
	})
	return found
}
