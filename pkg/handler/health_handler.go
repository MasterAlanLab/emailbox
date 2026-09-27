package handler

import (
	"net/http"

	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

// HealthHandler 是账号有效性检测与失效账号清理的入口。
type HealthHandler struct{ service *service.HealthService }

func NewHealthHandler(s *service.HealthService) *HealthHandler {
	return &HealthHandler{service: s}
}

type submitCheckRequest struct {
	Scope      string   `json:"scope"`
	AccountIDs []string `json:"account_ids"`
	GroupIDs   []string `json:"group_ids"`
}

func (h *HealthHandler) SubmitBatch(c *echo.Context) error {
	var req submitCheckRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	job, err := h.service.SubmitBatch(c.Request().Context(),
		c.Param("tenantID"), middleware.UserID(c), req.Scope, req.AccountIDs, req.GroupIDs)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, job, "任务已提交")
}

func (h *HealthHandler) Stats(c *echo.Context) error {
	v, err := h.service.Stats(c.Request().Context(), c.Param("tenantID"), c.QueryParam("group_id"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "获取成功")
}

func (h *HealthHandler) DeleteInvalid(c *echo.Context) error {
	var req model.DeleteInvalidRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	n, err := h.service.DeleteInvalid(c.Request().Context(), c.Param("tenantID"), req)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, map[string]int{"deleted": n}, "已删除失效账号")
}
