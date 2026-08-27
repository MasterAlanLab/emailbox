package handler

import (
	"archive/zip"
	"context"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"

	"emailbox/pkg/mailer"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

type MessageHandler struct{ service *service.MessageService }

func NewMessageHandler(s *service.MessageService) *MessageHandler {
	return &MessageHandler{service: s}
}

// batchRequest 是批量标已读/删除的请求体。
type batchRequest struct {
	Items []mailer.MessageRef `json:"items"`
}

// maxBatchItems 限制单次批量操作的条数。放开的话一个请求就能让远端调用跑几分钟，
// 期间连接一直占着——批量场景应该走 P4 的任务系统，不是同步接口。
const maxBatchItems = 200

func folderParam(c *echo.Context) mailer.Folder {
	folder := mailer.Folder(strings.TrimSpace(c.Request().URL.Query().Get("folder")))
	if folder == "" {
		return mailer.FolderInbox
	}
	return folder
}

func (h *MessageHandler) List(c *echo.Context) error {
	q := c.Request().URL.Query()
	opt := mailer.ListOptions{
		Folder: folderParam(c),
		Skip:   atoiOrZero(q.Get("skip")),
		Top:    atoiOrZero(q.Get("top")),
	}
	result, err := h.service.List(c.Request().Context(),
		c.Param("tenantID"), c.Param("accountID"), opt)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, result, "获取成功")
}

func (h *MessageHandler) Detail(c *echo.Context) error {
	q := c.Request().URL.Query()
	detail, err := h.service.Detail(c.Request().Context(),
		c.Param("tenantID"), c.Param("accountID"),
		folderParam(c), c.Param("messageID"), q.Get("id_mode"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, detail, "获取成功")
}

func (h *MessageHandler) Attachment(c *echo.Context) error {
	q := c.Request().URL.Query()
	att, err := h.service.Attachment(c.Request().Context(),
		c.Param("tenantID"), c.Param("accountID"),
		folderParam(c), c.Param("messageID"), q.Get("id_mode"), c.Param("attachmentID"))
	if err != nil {
		return mailError(c, err)
	}
	return streamAttachment(c, att.Name, att.ContentType, att.Content)
}

// AttachmentsZip 把一封信的全部附件打包下载。
func (h *MessageHandler) AttachmentsZip(c *echo.Context) error {
	q := c.Request().URL.Query()
	ctx := c.Request().Context()
	tenantID, accountID := c.Param("tenantID"), c.Param("accountID")
	folder, messageID, idMode := folderParam(c), c.Param("messageID"), q.Get("id_mode")

	detail, err := h.service.Detail(ctx, tenantID, accountID, folder, messageID, idMode)
	if err != nil {
		return mailError(c, err)
	}
	if len(detail.Attachments) == 0 {
		return failure(c, http.StatusNotFound, fmt.Errorf("这封邮件没有附件"))
	}

	c.Response().Header().Set(echo.HeaderContentType, "application/zip")
	c.Response().Header().Set(echo.HeaderContentDisposition,
		contentDisposition(safeDownloadName(detail.Subject)+".zip"))
	c.Response().WriteHeader(http.StatusOK)

	writer := zip.NewWriter(c.Response())
	defer func() { _ = writer.Close() }()

	used := make(map[string]int, len(detail.Attachments))
	for _, meta := range detail.Attachments {
		att, err := h.service.Attachment(ctx, tenantID, accountID, folder, messageID, idMode, meta.ID)
		if err != nil {
			// 响应头已经发出去了，这里不能再改状态码。跳过取不到的那个，
			// 其余附件照样打包——半个压缩包比一个 500 有用。
			continue
		}
		entry, err := writer.Create(uniqueName(used, att.Name))
		if err != nil {
			return nil
		}
		if _, err := entry.Write(att.Content); err != nil {
			return nil
		}
	}
	return nil
}

// uniqueName 处理同名附件：一封信里出现两个 image.png 时，ZIP 里会互相覆盖。
func uniqueName(used map[string]int, name string) string {
	if name == "" {
		name = "attachment"
	}
	n := used[name]
	used[name] = n + 1
	if n == 0 {
		return name
	}
	ext := path.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), n, ext)
}

func (h *MessageHandler) MarkRead(c *echo.Context) error {
	return h.runBatch(c, h.service.MarkRead)
}

func (h *MessageHandler) Delete(c *echo.Context) error {
	return h.runBatch(c, h.service.Delete)
}

func (h *MessageHandler) runBatch(
	c *echo.Context,
	run func(ctx context.Context, tenantID, accountID string, items []mailer.MessageRef) (mailer.BatchResult, error),
) error {
	var req batchRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	if len(req.Items) == 0 {
		return failure(c, http.StatusBadRequest, fmt.Errorf("items 不能为空"))
	}
	if len(req.Items) > maxBatchItems {
		return failure(c, http.StatusBadRequest,
			fmt.Errorf("单次最多 %d 封，更多请用批量任务", maxBatchItems))
	}
	result, err := run(c.Request().Context(), c.Param("tenantID"), c.Param("accountID"), req.Items)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, result, "操作完成")
}

// streamAttachment 把附件作为二进制流回传。
//
// 附件名完全由发件人控制，直接拼进响应头会造成头注入；
// 服务层已经净化过文件名，这里再按 RFC 5987 编码一次。
func streamAttachment(c *echo.Context, name, contentType string, content []byte) error {
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	c.Response().Header().Set(echo.HeaderContentDisposition,
		contentDisposition(safeDownloadName(name)))
	// nosniff 防止浏览器把 text/plain 的附件当 HTML 渲染。
	c.Response().Header().Set("X-Content-Type-Options", "nosniff")
	return c.Blob(http.StatusOK, contentType, content)
}

func contentDisposition(name string) string {
	// filename 给老客户端，filename* 给支持 RFC 5987 的，中文名靠后者。
	return fmt.Sprintf(`attachment; filename=%s; filename*=UTF-8''%s`,
		strconv.Quote(asciiFallback(name)), urlEscape(name))
}

// safeDownloadName 去掉换行与路径分隔符，避免响应头注入与路径穿越。
func safeDownloadName(name string) string {
	name = strings.Map(func(r rune) rune {
		switch r {
		case '\r', '\n', 0:
			return -1
		case '/', '\\':
			return '_'
		default:
			return r
		}
	}, strings.TrimSpace(name))
	if name == "" {
		return "attachment"
	}
	return name
}

// asciiFallback 把非 ASCII 字符换成 _，供不支持 filename* 的客户端使用。
func asciiFallback(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}
	if b.Len() == 0 {
		return "attachment"
	}
	return b.String()
}

func urlEscape(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for _, c := range []byte(s) {
		if isUnreservedByte(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&0x0f])
	}
	return b.String()
}

func isUnreservedByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '-' || c == '.' || c == '_' || c == '~':
		return true
	}
	return false
}
