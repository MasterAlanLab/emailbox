package imapx

import (
	"context"
	"strconv"
	"strings"

	"emailbox/pkg/mailer"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
)

// numSetFor 按 idMode 把 id 串换成对应的 NumSet。
//
// 这是 UID/序列号那个坑的唯一出入口：所有取单封邮件的路径都必须经过它，
// 各处自己解析 id 迟早会有一处忘了看 idMode，然后取到错误的邮件。
func numSetFor(id, idMode string) (imap.NumSet, error) {
	n, err := strconv.ParseUint(strings.TrimSpace(id), 10, 32)
	if err != nil || n == 0 {
		return nil, err
	}
	if idMode == mailer.IDModeUID {
		return imap.UIDSetNum(imap.UID(n)), nil
	}
	return imap.SeqSetNum(uint32(n)), nil
}

// Detail 实现 mailer.Client。
func (c *Client) Detail(
	ctx context.Context, cred mailer.Credential, folder mailer.Folder, id, idMode string,
) (*mailer.Detail, error) {
	return withSession(ctx, c, cred, func(_ context.Context, s *session) (*mailer.Detail, error) {
		if _, err := c.selectFolder(s, cred, folder); err != nil {
			return nil, err
		}
		numSet, err := numSetFor(id, idMode)
		if err != nil || numSet == nil {
			return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "邮件 id %q 非法", id)
		}

		// BODY.PEEK[] 取整封原文：PEEK 保证不会把邮件意外标成已读。
		// 详情页确实需要全文（正文 + 内嵌图片），列表页才是用 ENVELOPE 省带宽的地方。
		section := &imap.FetchItemBodySection{Peek: true}
		buffers, err := s.imap.Fetch(numSet, &imap.FetchOptions{
			UID:          true,
			Flags:        true,
			InternalDate: true,
			BodySection:  []*imap.FetchItemBodySection{section},
		}).Collect()
		if err != nil {
			return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "抓取邮件详情失败")
		}
		if len(buffers) == 0 {
			return nil, newError(c.cfg.Channel, mailer.ErrKindFolderUnavailable, "邮件不存在", nil)
		}

		buf := buffers[0]
		raw := buf.FindBodySection(section)
		parsed, err := ParseMessage(raw)
		if err != nil {
			return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "解析邮件失败")
		}
		return buildDetail(buf, parsed, folder, id, idMode), nil
	})
}

func buildDetail(
	buf *imapclient.FetchMessageBuffer, parsed *ParsedMessage,
	folder mailer.Folder, id, idMode string,
) *mailer.Detail {
	body, bodyType := parsed.Body()
	preview := parsed.TextBody
	if bodyType == "html" {
		preview = HTMLToPreview(parsed.HTMLBody, previewLimit)
	}

	detail := &mailer.Detail{
		Message: mailer.Message{
			ID:             id,
			IDMode:         idMode,
			Folder:         folder,
			Subject:        parsed.Subject,
			From:           parsed.From,
			To:             parsed.To,
			Cc:             parsed.Cc,
			ReceivedAt:     parsed.Date,
			HasAttachments: len(parsed.Attachments) > 0,
			BodyPreview:    preview,
		},
		Body:     body,
		BodyType: bodyType,
	}
	if !buf.InternalDate.IsZero() {
		detail.ReceivedAt = buf.InternalDate
	}
	for _, flag := range buf.Flags {
		if flag == imap.FlagSeen {
			detail.IsRead = true
		}
	}
	// 附件 id 用序号：IMAP 的分段没有稳定标识符，只能靠在这封信里的位置定位。
	for i, att := range parsed.Attachments {
		detail.Attachments = append(detail.Attachments, mailer.AttachmentMeta{
			ID:          strconv.Itoa(i),
			Name:        att.Name,
			ContentType: att.ContentType,
			Size:        int64(len(att.Content)),
			IsInline:    att.IsInline,
		})
	}
	return detail
}

// Attachment 实现 mailer.Client。
//
// IMAP 的分段没有稳定 id，所以这里重新抓一次整封信再按序号取。
// 比 Graph 的直接下载贵，但正确：按 BODYSTRUCTURE 的路径去 FETCH 单个分段，
// 在多层嵌套（转发带附件）的信上很容易取错分段。
func (c *Client) Attachment(
	ctx context.Context, cred mailer.Credential, folder mailer.Folder, msgID, idMode, attID string,
) (*mailer.Attachment, error) {
	index, err := strconv.Atoi(strings.TrimSpace(attID))
	if err != nil || index < 0 {
		return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "附件 id %q 非法", attID)
	}

	return withSession(ctx, c, cred, func(_ context.Context, s *session) (*mailer.Attachment, error) {
		if _, err := c.selectFolder(s, cred, folder); err != nil {
			return nil, err
		}
		numSet, err := numSetFor(msgID, idMode)
		if err != nil || numSet == nil {
			return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "邮件 id %q 非法", msgID)
		}
		section := &imap.FetchItemBodySection{Peek: true}
		buffers, err := s.imap.Fetch(numSet, &imap.FetchOptions{
			BodySection: []*imap.FetchItemBodySection{section},
		}).Collect()
		if err != nil {
			return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "抓取邮件失败")
		}
		if len(buffers) == 0 {
			return nil, newError(c.cfg.Channel, mailer.ErrKindFolderUnavailable, "邮件不存在", nil)
		}
		parsed, err := ParseMessage(buffers[0].FindBodySection(section))
		if err != nil {
			return nil, errorf(c.cfg.Channel, mailer.ErrKindProviderError, err, "解析邮件失败")
		}
		if index >= len(parsed.Attachments) {
			return nil, newError(c.cfg.Channel, mailer.ErrKindFolderUnavailable, "附件不存在", nil)
		}
		att := parsed.Attachments[index]
		return &mailer.Attachment{
			AttachmentMeta: mailer.AttachmentMeta{
				ID:          attID,
				Name:        att.Name,
				ContentType: att.ContentType,
				Size:        int64(len(att.Content)),
				IsInline:    att.IsInline,
			},
			Content: att.Content,
		}, nil
	})
}

// MarkRead 实现 mailer.Client。
func (c *Client) MarkRead(
	ctx context.Context, cred mailer.Credential, items []mailer.MessageRef,
) (mailer.BatchResult, error) {
	return c.batchOp(ctx, cred, items, func(s *session, numSet imap.NumSet) error {
		return s.imap.Store(numSet, &imap.StoreFlags{
			Op:     imap.StoreFlagsAdd,
			Silent: true,
			Flags:  []imap.Flag{imap.FlagSeen},
		}, nil).Close()
	})
}

// Delete 实现 mailer.Client。
//
// IMAP 的删除是「打 \Deleted 标记 + EXPUNGE」两步。只打标记不 EXPUNGE 的话
// 邮件还在，用户会以为没删掉；而 EXPUNGE 是对整个邮箱生效的，
// 所以必须在打完本批标记后立刻执行，不能攒到最后。
func (c *Client) Delete(
	ctx context.Context, cred mailer.Credential, items []mailer.MessageRef,
) (mailer.BatchResult, error) {
	return c.batchOp(ctx, cred, items, func(s *session, numSet imap.NumSet) error {
		if err := s.imap.Store(numSet, &imap.StoreFlags{
			Op:     imap.StoreFlagsAdd,
			Silent: true,
			Flags:  []imap.Flag{imap.FlagDeleted},
		}, nil).Close(); err != nil {
			return err
		}
		if uidSet, ok := numSet.(imap.UIDSet); ok && s.imap.Caps().Has(imap.CapUIDPlus) {
			// UIDPLUS 的 UID EXPUNGE 只清掉指定的那些，
			// 不会顺手清掉别的客户端刚标上 \Deleted 的邮件。
			return s.imap.UIDExpunge(uidSet).Close()
		}
		return s.imap.Expunge().Close()
	})
}

// batchOp 把一批邮件按邮件夹分组，每个邮件夹 SELECT 一次后批量执行。
//
// 按邮件夹分组是必要的：IMAP 一次只能选中一个邮箱，
// 逐封 SELECT 的话删 100 封信要 SELECT 100 次。
func (c *Client) batchOp(
	ctx context.Context, cred mailer.Credential, items []mailer.MessageRef,
	apply func(*session, imap.NumSet) error,
) (mailer.BatchResult, error) {
	if len(items) == 0 {
		return mailer.BatchResult{}, nil
	}
	return withSession(ctx, c, cred, func(_ context.Context, s *session) (mailer.BatchResult, error) {
		result := mailer.BatchResult{Items: make([]mailer.ItemResult, 0, len(items))}

		for _, group := range groupByFolder(items) {
			if _, err := c.selectFolder(s, cred, group.folder); err != nil {
				for _, item := range group.items {
					result.Failed++
					result.Items = append(result.Items, mailer.ItemResult{Ref: item, Error: err.Error()})
				}
				continue
			}
			uids, seqs, bad := splitByIDMode(group.items)
			for _, item := range bad {
				result.Failed++
				result.Items = append(result.Items, mailer.ItemResult{Ref: item, Error: "邮件 id 非法"})
			}
			for _, batch := range []struct {
				numSet imap.NumSet
				refs   []mailer.MessageRef
			}{
				{uids.set, uids.refs},
				{seqs.set, seqs.refs},
			} {
				if len(batch.refs) == 0 {
					continue
				}
				err := apply(s, batch.numSet)
				for _, item := range batch.refs {
					if err != nil {
						result.Failed++
						result.Items = append(result.Items, mailer.ItemResult{Ref: item, Error: err.Error()})
						continue
					}
					result.Succeeded++
					result.Items = append(result.Items, mailer.ItemResult{Ref: item, OK: true})
				}
			}
		}
		return result, nil
	})
}

type folderGroup struct {
	folder mailer.Folder
	items  []mailer.MessageRef
}

// groupByFolder 按邮件夹分组，保持首次出现的顺序。
func groupByFolder(items []mailer.MessageRef) []folderGroup {
	order := make([]mailer.Folder, 0, 3)
	byFolder := make(map[mailer.Folder][]mailer.MessageRef, 3)
	for _, item := range items {
		if _, ok := byFolder[item.Folder]; !ok {
			order = append(order, item.Folder)
		}
		byFolder[item.Folder] = append(byFolder[item.Folder], item)
	}
	out := make([]folderGroup, 0, len(order))
	for _, f := range order {
		out = append(out, folderGroup{folder: f, items: byFolder[f]})
	}
	return out
}

type numSetGroup struct {
	set  imap.NumSet
	refs []mailer.MessageRef
}

// splitByIDMode 把一组邮件按 UID / 序列号分开。
//
// 两套编号绝不能放进同一个 NumSet：那正是「取到 / 删到错误邮件」的来源。
func splitByIDMode(items []mailer.MessageRef) (uids, seqs numSetGroup, bad []mailer.MessageRef) {
	var uidSet imap.UIDSet
	var seqSet imap.SeqSet
	for _, item := range items {
		n, err := strconv.ParseUint(strings.TrimSpace(item.ID), 10, 32)
		if err != nil || n == 0 {
			bad = append(bad, item)
			continue
		}
		if item.IDMode == mailer.IDModeSequence {
			seqSet.AddNum(uint32(n))
			seqs.refs = append(seqs.refs, item)
			continue
		}
		uidSet.AddNum(imap.UID(n))
		uids.refs = append(uids.refs, item)
	}
	uids.set, seqs.set = uidSet, seqSet
	return uids, seqs, bad
}
