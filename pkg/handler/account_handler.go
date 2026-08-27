package handler

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

// AccountHandler 也持有 AuthService：导出接口要二次验证操作者的登录密码，
// 而密码哈希的比对只有 AuthService 做（bcrypt 参数与登录路径必须是同一套）。
type AccountHandler struct {
	service *service.AccountService
	auth    *service.AuthService
}

func NewAccountHandler(s *service.AccountService, auth *service.AuthService) *AccountHandler {
	return &AccountHandler{service: s, auth: auth}
}

// parseAccountFilter 从查询参数构造筛选条件。
// 非法取值一律回落到默认值而不是报错——列表页的筛选参数常来自书签或分享链接，
// 因为一个过期的参数就返回 400 反而更难用。取值合法性由 Normalize 兜底。
func parseAccountFilter(c *echo.Context) model.AccountFilter {
	q := c.Request().URL.Query()
	filter := model.AccountFilter{
		Query:         strings.TrimSpace(q.Get("q")),
		Status:        q.Get("status"),
		RefreshStatus: q.Get("refresh_status"),
		Provider:      q.Get("provider"),
		Sort:          q.Get("sort"),
		Order:         q.Get("order"),
	}
	if groupID := q.Get("group_id"); groupID != "" {
		filter.GroupIDs = []string{groupID}
	}
	// 解析失败留 0，由 Normalize 回落到默认页码与页大小。
	filter.Page = atoiOrZero(q.Get("page"))
	filter.Limit = atoiOrZero(q.Get("limit"))
	filter.Normalize()
	return filter
}

func atoiOrZero(s string) int {
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return v
}

func (h *AccountHandler) List(c *echo.Context) error {
	filter := parseAccountFilter(c)
	items, total, err := h.service.List(c.Request().Context(), c.Param("tenantID"), filter)
	if err != nil {
		return mailError(c, err)
	}
	pages := 0
	if filter.Limit > 0 {
		pages = (total + filter.Limit - 1) / filter.Limit
	}
	return success(c, map[string]any{
		"items": items,
		"pagination": map[string]any{
			"page": filter.Page, "limit": filter.Limit, "total": total, "pages": pages,
		},
	}, "获取成功")
}

func (h *AccountHandler) Get(c *echo.Context) error {
	v, err := h.service.Get(c.Request().Context(), c.Param("tenantID"), c.Param("accountID"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "获取成功")
}

func (h *AccountHandler) Create(c *echo.Context) error {
	var req model.CreateMailAccountRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	v, err := h.service.Create(c.Request().Context(), c.Param("tenantID"), req)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "创建成功")
}

func (h *AccountHandler) Update(c *echo.Context) error {
	var req model.UpdateMailAccountRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	v, err := h.service.Update(c.Request().Context(), c.Param("tenantID"), c.Param("accountID"), req)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "更新成功")
}

func (h *AccountHandler) Delete(c *echo.Context) error {
	if err := h.service.Delete(c.Request().Context(), c.Param("tenantID"), c.Param("accountID")); err != nil {
		return mailError(c, err)
	}
	return success(c, nil, "删除成功")
}

func (h *AccountHandler) Import(c *echo.Context) error {
	var req model.ImportAccountsRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	v, err := h.service.Import(c.Request().Context(), c.Param("tenantID"), req)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "导入完成")
}

// Export 导出账号凭据（05 文档 §4.4）。返回 text/plain 附件，格式与导入一致。
//
// 二次密码验证在这里做而不是在 service 里：验的是**操作者本人**的密码，
// 与被导出的租户无关——管理员导出他人租户时验的仍是管理员自己的密码。
func (h *AccountHandler) Export(c *echo.Context) error {
	var req model.ExportAccountsRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	ctx := c.Request().Context()
	if err := h.auth.VerifyPassword(ctx, middleware.UserID(c), req.PasswordConfirm); err != nil {
		if errors.Is(err, service.ErrPasswordMismatch) {
			return failure(c, http.StatusForbidden, err)
		}
		return failure(c, http.StatusInternalServerError, err)
	}
	content, count, err := h.service.Export(ctx, c.Param("tenantID"), req)
	if err != nil {
		return mailError(c, err)
	}
	filename := "accounts-" + time.Now().Format("20060102-150405") + ".txt"
	c.Response().Header().Set(echo.HeaderContentDisposition, `attachment; filename="`+filename+`"`)
	// 导出条数走响应头：响应体本身是给用户下载的文件，塞进统计信息就不能直接重新导入了。
	c.Response().Header().Set("X-Export-Count", strconv.Itoa(count))
	return c.Blob(http.StatusOK, echo.MIMETextPlainCharsetUTF8, []byte(content))
}

func (h *AccountHandler) BatchMove(c *echo.Context) error {
	return h.batch(c, func(tenantID string) (*model.BatchResult, error) {
		var req model.BatchMoveRequest
		if err := c.Bind(&req); err != nil {
			return nil, err
		}
		return h.service.BatchMove(c.Request().Context(), tenantID, req)
	})
}

func (h *AccountHandler) BatchStatus(c *echo.Context) error {
	return h.batch(c, func(tenantID string) (*model.BatchResult, error) {
		var req model.BatchStatusRequest
		if err := c.Bind(&req); err != nil {
			return nil, err
		}
		return h.service.BatchStatus(c.Request().Context(), tenantID, req)
	})
}

func (h *AccountHandler) BatchProxy(c *echo.Context) error {
	return h.batch(c, func(tenantID string) (*model.BatchResult, error) {
		var req model.BatchProxyRequest
		if err := c.Bind(&req); err != nil {
			return nil, err
		}
		return h.service.BatchProxy(c.Request().Context(), tenantID, req)
	})
}

func (h *AccountHandler) BatchDelete(c *echo.Context) error {
	return h.batch(c, func(tenantID string) (*model.BatchResult, error) {
		var req model.BatchDeleteRequest
		if err := c.Bind(&req); err != nil {
			return nil, err
		}
		return h.service.BatchDelete(c.Request().Context(), tenantID, req)
	})
}

// batch 收拢批量接口共同的绑定与错误映射。
func (h *AccountHandler) batch(c *echo.Context, run func(tenantID string) (*model.BatchResult, error)) error {
	v, err := run(c.Param("tenantID"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "操作完成")
}
