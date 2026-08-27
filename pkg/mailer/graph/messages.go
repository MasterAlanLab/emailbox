package graph

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strings"
	"time"

	"emailbox/pkg/mailer"
)

// $select 列表。显式列字段而不是取全量：一封信的完整 JSON 有几十 KB，
// 列表页取 50 条就是几 MB，绝大部分字段前端根本用不到。
const (
	listSelect   = "id,subject,from,toRecipients,receivedDateTime,isRead,hasAttachments,bodyPreview"
	detailSelect = listSelect + ",ccRecipients,body"
)

// folderPath 把领域层的邮件夹映射到 Graph 的 well-known folder name。
func folderPath(f mailer.Folder) (string, error) {
	switch f {
	case mailer.FolderInbox:
		return "inbox", nil
	case mailer.FolderJunk:
		return "junkemail", nil
	case mailer.FolderDeleted:
		return "deleteditems", nil
	case mailer.FolderAll:
		// FolderAll 不对应任何真实邮件夹，必须由上层拆成多次调用后归并。
		// 走到这里说明调用方漏了这步，明确报错好过悄悄只返回收件箱。
		return "", newError(mailer.ErrKindProviderError,
			"folder=all 需要由上层拆分后再调用协议层", 0, nil)
	default:
		return "", newError(mailer.ErrKindFolderUnavailable,
			fmt.Sprintf("不支持的邮件夹 %q", f), 0, nil)
	}
}

type graphRecipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

func (r graphRecipient) String() string {
	addr := strings.TrimSpace(r.EmailAddress.Address)
	name := strings.TrimSpace(r.EmailAddress.Name)
	switch {
	case addr == "":
		return name
	case name == "" || name == addr:
		return addr
	default:
		return name + " <" + addr + ">"
	}
}

func joinRecipients(list []graphRecipient) string {
	if len(list) == 0 {
		return ""
	}
	parts := make([]string, 0, len(list))
	for _, r := range list {
		if s := r.String(); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

type graphMessage struct {
	ID               string           `json:"id"`
	Subject          string           `json:"subject"`
	From             graphRecipient   `json:"from"`
	ToRecipients     []graphRecipient `json:"toRecipients"`
	CcRecipients     []graphRecipient `json:"ccRecipients"`
	ReceivedDateTime time.Time        `json:"receivedDateTime"`
	IsRead           bool             `json:"isRead"`
	HasAttachments   bool             `json:"hasAttachments"`
	BodyPreview      string           `json:"bodyPreview"`
	Body             struct {
		ContentType string `json:"contentType"`
		Content     string `json:"content"`
	} `json:"body"`
}

func (m graphMessage) toMessage(folder mailer.Folder) mailer.Message {
	return mailer.Message{
		ID: m.ID,
		// Graph 的 message id 是全局唯一字符串，没有 UID/序列号之分。
		IDMode:         mailer.IDModeNone,
		Folder:         folder,
		Subject:        m.Subject,
		From:           m.From.String(),
		To:             joinRecipients(m.ToRecipients),
		Cc:             joinRecipients(m.CcRecipients),
		ReceivedAt:     m.ReceivedDateTime,
		IsRead:         m.IsRead,
		HasAttachments: m.HasAttachments,
		BodyPreview:    m.BodyPreview,
	}
}

type graphAttachment struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	ContentType  string `json:"contentType"`
	Size         int64  `json:"size"`
	IsInline     bool   `json:"isInline"`
	ContentBytes string `json:"contentBytes"`
}

func (a graphAttachment) toMeta() mailer.AttachmentMeta {
	return mailer.AttachmentMeta{
		ID:          a.ID,
		Name:        a.Name,
		ContentType: a.ContentType,
		Size:        a.Size,
		IsInline:    a.IsInline,
	}
}

// List 实现 mailer.Client。
func (c *Client) List(ctx context.Context, cred mailer.Credential, opt mailer.ListOptions) ([]mailer.Message, error) {
	folder, err := folderPath(opt.Folder)
	if err != nil {
		return nil, err
	}
	top := opt.Top
	if top <= 0 {
		top = 20
	}
	query := url.Values{
		"$top":     {fmt.Sprint(top)},
		"$orderby": {"receivedDateTime desc"},
		"$select":  {listSelect},
	}
	if opt.Skip > 0 {
		query.Set("$skip", fmt.Sprint(opt.Skip))
	}
	path := "/me/mailFolders/" + folder + "/messages?" + query.Encode()

	return withSession(ctx, c, cred, func(ctx context.Context, s *session) ([]mailer.Message, error) {
		var payload struct {
			Value []graphMessage `json:"value"`
		}
		if err := c.doJSON(ctx, s, "GET", path, nil, &payload); err != nil {
			return nil, err
		}
		out := make([]mailer.Message, 0, len(payload.Value))
		for _, m := range payload.Value {
			out = append(out, m.toMessage(opt.Folder))
		}
		return out, nil
	})
}

// Detail 实现 mailer.Client。idMode 对 Graph 无意义，仅为满足接口。
func (c *Client) Detail(
	ctx context.Context, cred mailer.Credential, folder mailer.Folder, id, _ string,
) (*mailer.Detail, error) {
	if strings.TrimSpace(id) == "" {
		return nil, newError(mailer.ErrKindProviderError, "缺少邮件 id", 0, nil)
	}
	messagePath := "/me/messages/" + url.PathEscape(id) + "?$select=" + url.QueryEscape(detailSelect)
	attachmentPath := "/me/messages/" + url.PathEscape(id) +
		"/attachments?$select=" + url.QueryEscape("id,name,contentType,size,isInline")

	return withSession(ctx, c, cred, func(ctx context.Context, s *session) (*mailer.Detail, error) {
		var raw graphMessage
		if err := c.doJSON(ctx, s, "GET", messagePath, nil, &raw); err != nil {
			return nil, err
		}
		detail := &mailer.Detail{
			Message:  raw.toMessage(folder),
			Body:     raw.Body.Content,
			BodyType: strings.ToLower(raw.Body.ContentType),
		}
		if detail.BodyType == "" {
			detail.BodyType = "text"
		}
		if !raw.HasAttachments {
			return detail, nil
		}
		var payload struct {
			Value []graphAttachment `json:"value"`
		}
		if err := c.doJSON(ctx, s, "GET", attachmentPath, nil, &payload); err != nil {
			// 附件列表取不到不该让整封信打不开——正文才是用户要看的东西。
			// 附件区会是空的，比一个「加载失败」的空白页有用得多。
			return detail, nil
		}
		for _, a := range payload.Value {
			detail.Attachments = append(detail.Attachments, a.toMeta())
		}
		return detail, nil
	})
}

// Attachment 实现 mailer.Client。idMode 对 Graph 无意义，仅为满足接口。
func (c *Client) Attachment(
	ctx context.Context, cred mailer.Credential, _ mailer.Folder, msgID, _, attID string,
) (*mailer.Attachment, error) {
	if strings.TrimSpace(msgID) == "" || strings.TrimSpace(attID) == "" {
		return nil, newError(mailer.ErrKindProviderError, "缺少邮件 id 或附件 id", 0, nil)
	}
	path := "/me/messages/" + url.PathEscape(msgID) + "/attachments/" + url.PathEscape(attID)

	return withSession(ctx, c, cred, func(ctx context.Context, s *session) (*mailer.Attachment, error) {
		var raw graphAttachment
		if err := c.doJSON(ctx, s, "GET", path, nil, &raw); err != nil {
			return nil, err
		}
		content, err := base64.StdEncoding.DecodeString(raw.ContentBytes)
		if err != nil {
			return nil, newError(mailer.ErrKindProviderError, "附件内容解码失败", 0, err)
		}
		meta := raw.toMeta()
		if meta.Size == 0 {
			meta.Size = int64(len(content))
		}
		return &mailer.Attachment{AttachmentMeta: meta, Content: content}, nil
	})
}
