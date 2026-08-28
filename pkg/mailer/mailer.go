package mailer

import (
	"context"
	"time"
)

// Folder 是邮件夹。取值是 Graph 的命名，IMAP 侧由 folder 解析表映射到各服务商的实际名称。
type Folder string

const (
	FolderInbox   Folder = "inbox"
	FolderJunk    Folder = "junkemail"
	FolderDeleted Folder = "deleteditems"
	// FolderAll 聚合收件箱与垃圾箱，按时间倒序。它不对应任何真实邮件夹，
	// 由上层分别拉取后归并。
	FolderAll Folder = "all"
)

// ValidFolder 判断取值是否为支持的邮件夹。
func ValidFolder(f Folder) bool {
	switch f {
	case FolderInbox, FolderJunk, FolderDeleted, FolderAll:
		return true
	}
	return false
}

// ID 模式。IMAP 的 UID 与序列号是两套编号，混用会取到错误的邮件——
// outlookEmail 修过这个真实 bug，所以列表返回的 IDMode 必须如实标注，
// 详情与附件请求也必须带回同一个值。
const (
	IDModeUID      = "uid"
	IDModeSequence = "sequence"
	// IDModeNone 用于 Graph：它的 message id 是全局唯一字符串，没有这个概念。
	IDModeNone = ""
)

// 通道名。写回 mail_accounts.auth_channel，下次优先尝试。
const (
	ChannelGraph   = "graph"
	ChannelIMAPNew = "imap_new"
	ChannelIMAPOld = "imap_old"
	// ChannelIMAP 用于非 Outlook 的密码鉴权账号，它只有这一条通道。
	ChannelIMAP = "imap"
)

// Credential 是解密后的账号凭据，只在内存里流转。
// mailer 包不碰数据库：由 service 层取出账号、解密、组装成它再调用。
type Credential struct {
	Email        string
	Provider     string
	AccountType  AccountType
	ClientID     string
	ClientSecret string
	RefreshToken string
	Password     string
	IMAPHost     string
	IMAPPort     int
	IMAPPassword string
	// AuthChannel 是上次成功的通道，回退链会把它提到最前面先试。
	AuthChannel string
	Proxy       ProxyConfig
}

// Message 是邮件列表里的一条。
type Message struct {
	ID             string    `json:"id"`
	IDMode         string    `json:"id_mode"`
	Folder         Folder    `json:"folder"`
	Subject        string    `json:"subject"`
	From           string    `json:"from"`
	To             string    `json:"to"`
	Cc             string    `json:"cc"`
	ReceivedAt     time.Time `json:"received_at"`
	IsRead         bool      `json:"is_read"`
	HasAttachments bool      `json:"has_attachments"`
	BodyPreview    string    `json:"body_preview"`
}

// AttachmentMeta 是附件的元信息，不含内容。
type AttachmentMeta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	IsInline    bool   `json:"is_inline"`
}

// Detail 是邮件详情。
type Detail struct {
	Message
	Body     string `json:"body"`
	BodyType string `json:"body_type"` // text | html
	// Attachments 只有元信息；内容通过 Attachment 单独取，避免详情响应体过大。
	Attachments []AttachmentMeta `json:"attachments"`
}

// Attachment 是附件内容。
type Attachment struct {
	AttachmentMeta
	Content []byte
}

// MessageRef 指向一封邮件，供标已读/删除这类批量操作使用。
type MessageRef struct {
	ID     string `json:"id"`
	IDMode string `json:"id_mode"`
	Folder Folder `json:"folder"`
}

// ListOptions 是列表请求的参数。
type ListOptions struct {
	Folder Folder
	Skip   int
	Top    int
}

// ItemResult 是批量操作里单封邮件的结果。
type ItemResult struct {
	Ref   MessageRef `json:"ref"`
	OK    bool       `json:"ok"`
	Error string     `json:"error,omitempty"`
}

// BatchResult 是批量操作的汇总。
type BatchResult struct {
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
	Items     []ItemResult `json:"items"`
}

// Client 是一个邮件通道。graph / imapx 各实现一份，chain 组合它们并实现回退，
// 对外只暴露这一个接口。
//
// 所有方法都收 context：超时由 service 层用 OverallTimeout 统一控制。
// 只在单次调用上设超时是不够的——整条回退链（3 个通道各自超时）会累计到两分钟以上。
type Client interface {
	// Channel 返回该实现的通道名，用于写回 auth_channel 与错误归因。
	Channel() string
	List(ctx context.Context, cred Credential, opt ListOptions) ([]Message, error)
	Detail(ctx context.Context, cred Credential, folder Folder, id, idMode string) (*Detail, error)
	// Attachment 的 idMode 说的是 msgID 的编号方式，不是附件的。
	// 它必须与列表里返回的 IDMode 一致：IMAP 上按 UID 去取序列号标识的邮件
	// （或反过来）会取到另一封信的附件。
	Attachment(ctx context.Context, cred Credential, folder Folder, msgID, idMode, attID string) (*Attachment, error)
	MarkRead(ctx context.Context, cred Credential, items []MessageRef) (BatchResult, error)
	Delete(ctx context.Context, cred Credential, items []MessageRef) (BatchResult, error)
}
