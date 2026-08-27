package imapx

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/htmlindex"
	"golang.org/x/text/transform"
)

// 单封邮件解析出来的内容上限。畸形或恶意邮件可以声称自己有几百 MB，
// 批量拉信时几个这样的就能把进程打爆。
const (
	maxBodyBytes       = 2 << 20  // 2 MiB 正文
	maxAttachmentBytes = 25 << 20 // 25 MiB 单附件
	maxAttachments     = 100
	maxMultipartDepth  = 10
)

// ParsedAttachment 是解析出来的一个附件。
type ParsedAttachment struct {
	Name        string
	ContentType string
	ContentID   string
	IsInline    bool
	Content     []byte
}

// ParsedMessage 是一封邮件解析后的结果。
type ParsedMessage struct {
	Subject     string
	From        string
	To          string
	Cc          string
	Date        time.Time
	HTMLBody    string
	TextBody    string
	Attachments []ParsedAttachment
}

// Body 返回展示用的正文与类型：优先 HTML，回退纯文本。
func (m *ParsedMessage) Body() (body, bodyType string) {
	if strings.TrimSpace(m.HTMLBody) != "" {
		return m.HTMLBody, "html"
	}
	return m.TextBody, "text"
}

// charsetReader 让 mime 与 multipart 认识 GBK/GB2312/Big5 这些中文邮箱常见的编码。
//
// Python 的 decode_header 默认就能处理这些，Go 必须显式接 x/text——
// 漏了的话中文主题会变成一串问号或乱码，而且只在特定服务商上出现。
func charsetReader(charset string, input io.Reader) (io.Reader, error) {
	name := strings.ToLower(strings.TrimSpace(charset))
	switch name {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return input, nil
	}
	enc, err := htmlindex.Get(name)
	if err != nil {
		return nil, fmt.Errorf("imapx: 不支持的字符集 %q: %w", charset, err)
	}
	return transform.NewReader(input, enc.NewDecoder()), nil
}

var wordDecoder = &mime.WordDecoder{CharsetReader: charsetReader}

// DecodeHeader 解码 =?GBK?B?...?= 这类编码字。
//
// 解不出来时返回原文而不是空串：一个乱码的主题至少还能看出是哪封信，
// 空主题会让用户以为邮件损坏了。
func DecodeHeader(s string) string {
	if s == "" {
		return ""
	}
	decoded, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return decoded
}

// decodeAddressList 把地址头解成 "名字 <地址>, 名字 <地址>" 的展示串。
func decodeAddressList(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}
	parser := mail.AddressParser{WordDecoder: wordDecoder}
	addrs, err := parser.ParseList(raw)
	if err != nil {
		// 畸形地址头很常见（尤其是营销邮件），退回解码后的原文，
		// 总好过把整封信判成解析失败。
		return DecodeHeader(raw)
	}
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name == "" {
			parts = append(parts, a.Address)
			continue
		}
		parts = append(parts, a.Name+" <"+a.Address+">")
	}
	return strings.Join(parts, ", ")
}

// ParseMessage 解析一封完整的 RFC 822 邮件。
func ParseMessage(raw []byte) (*ParsedMessage, error) {
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("imapx: 解析邮件失败: %w", err)
	}
	out := &ParsedMessage{
		Subject: DecodeHeader(msg.Header.Get("Subject")),
		From:    decodeAddressList(msg.Header.Get("From")),
		To:      decodeAddressList(msg.Header.Get("To")),
		Cc:      decodeAddressList(msg.Header.Get("Cc")),
	}
	if date, err := msg.Header.Date(); err == nil {
		out.Date = date
	}
	if err := parsePart(out, headerFunc(msg.Header.Get), msg.Body, 0); err != nil {
		return nil, err
	}
	return out, nil
}

// partHeader 把 mail.Header 与 multipart.Part 的头统一成一个接口，
// 这样递归解析只用写一份。
type partHeader interface {
	Get(key string) string
}

type headerFunc func(string) string

func (f headerFunc) Get(key string) string { return f(key) }

// parsePart 递归解析一个 MIME 分段。
func parsePart(out *ParsedMessage, header partHeader, body io.Reader, depth int) error {
	if depth > maxMultipartDepth {
		// 嵌套过深要么是畸形邮件，要么是有人故意构造的解析炸弹。
		return nil
	}
	contentType := header.Get("Content-Type")
	if contentType == "" {
		contentType = "text/plain"
	}
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		mediaType, params = "text/plain", map[string]string{}
	}

	if strings.HasPrefix(mediaType, "multipart/") {
		boundary := params["boundary"]
		if boundary == "" {
			return nil
		}
		reader := multipart.NewReader(body, boundary)
		for {
			part, err := reader.NextPart()
			if err == io.EOF {
				return nil
			}
			if err != nil {
				// 截断的 multipart 很常见（尤其是分页抓取时）。
				// 已经解出来的部分照样返回，不要整封信作废。
				return nil
			}
			if err := parsePart(out, part.Header, part, depth+1); err != nil {
				_ = part.Close()
				return err
			}
			_ = part.Close()
		}
	}

	// Content-Disposition 缺失或畸形都很常见，解不出来就按「不是附件」处理，
	// 下面还有 name 参数兜底。
	disposition, dispParams, dispErr := mime.ParseMediaType(header.Get("Content-Disposition"))
	if dispErr != nil {
		disposition, dispParams = "", map[string]string{}
	}
	filename := dispParams["filename"]
	if filename == "" {
		filename = params["name"]
	}
	isAttachment := strings.EqualFold(disposition, "attachment") || filename != ""

	decoded := decodeTransferEncoding(header.Get("Content-Transfer-Encoding"), body)

	if isAttachment {
		return collectAttachment(out, header, mediaType, filename, disposition, decoded)
	}
	return collectBody(out, mediaType, params["charset"], decoded)
}

func collectAttachment(
	out *ParsedMessage, header partHeader, mediaType, filename, disposition string, body io.Reader,
) error {
	if len(out.Attachments) >= maxAttachments {
		return nil
	}
	content, err := io.ReadAll(io.LimitReader(body, maxAttachmentBytes))
	if err != nil {
		return nil
	}
	out.Attachments = append(out.Attachments, ParsedAttachment{
		Name:        SanitizeAttachmentFilename(DecodeHeader(filename)),
		ContentType: mediaType,
		ContentID:   strings.Trim(header.Get("Content-Id"), "<>"),
		IsInline:    strings.EqualFold(disposition, "inline"),
		Content:     content,
	})
	return nil
}

func collectBody(out *ParsedMessage, mediaType, charset string, body io.Reader) error {
	if mediaType != "text/plain" && mediaType != "text/html" {
		return nil
	}
	reader, err := charsetReader(charset, io.LimitReader(body, maxBodyBytes))
	if err != nil {
		// 认不出的字符集按原样读：乱码好过丢正文。
		reader = io.LimitReader(body, maxBodyBytes)
	}
	raw, err := io.ReadAll(reader)
	if err != nil {
		return nil
	}
	text := string(raw)
	// 一封信可能有多个同类型分段（转发链），拼起来而不是后者覆盖前者。
	if mediaType == "text/html" {
		out.HTMLBody += text
		return nil
	}
	out.TextBody += text
	return nil
}

func decodeTransferEncoding(encoding string, body io.Reader) io.Reader {
	switch strings.ToLower(strings.TrimSpace(encoding)) {
	case "quoted-printable":
		return quotedprintable.NewReader(body)
	case "base64":
		return newLenientBase64Reader(body)
	default:
		return body
	}
}

var unsafeFilenameChars = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SanitizeAttachmentFilename 把附件名清理成安全的文件名。
//
// 附件名完全由发件人控制。不清理的话 ../../etc/passwd 这种名字会在下载或
// 打包 ZIP 时写到目录外——这是附件功能上最直接的一个攻击面。
func SanitizeAttachmentFilename(name string) string {
	name = strings.TrimSpace(name)
	// 先按两种分隔符各取一次最后一段：Windows 的反斜杠在 Linux 上不被 filepath 当分隔符。
	name = name[strings.LastIndexAny(name, `/\`)+1:]
	name = filepath.Base(name)

	name = unsafeFilenameChars.ReplaceAllString(name, "_")
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return '_'
		}
		return r
	}, name)
	name = strings.Trim(name, " .")

	// . 与 .. 被上面剥成空串，以及本来就没名字的内嵌图片。
	if name == "" {
		return "attachment"
	}
	// 文件系统的名字上限通常是 255 字节，按 rune 截断避免切出半个字符。
	if len(name) > 200 {
		trimmed := name[:200]
		for len(trimmed) > 0 && !utf8.ValidString(trimmed) {
			trimmed = trimmed[:len(trimmed)-1]
		}
		name = trimmed
	}
	return name
}

var htmlTag = regexp.MustCompile(`(?s)<(script|style)\b[^>]*>.*?</\s*(script|style)\s*>|<[^>]*>`)

// HTMLToPreview 把 HTML 正文压成一行纯文本，用于列表页的摘要。
//
// 这里只做摘要，不负责安全：正文渲染走前端的 DOMPurify + sandbox iframe 双层。
func HTMLToPreview(html string, limit int) string {
	text := htmlTag.ReplaceAllString(html, " ")
	text = htmlUnescape(text)
	text = strings.Join(strings.Fields(text), " ")
	if limit > 0 && utf8.RuneCountInString(text) > limit {
		runes := []rune(text)
		return string(runes[:limit]) + "…"
	}
	return text
}

var htmlEntities = strings.NewReplacer(
	"&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">",
	"&quot;", `"`, "&#39;", "'", "&apos;", "'",
)

func htmlUnescape(s string) string { return htmlEntities.Replace(s) }
