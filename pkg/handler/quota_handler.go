package handler

import (
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

type QuotaHandler struct{ service *service.QuotaService }

func NewQuotaHandler(s *service.QuotaService) *QuotaHandler { return &QuotaHandler{service: s} }

func (h *QuotaHandler) Get(c *echo.Context) error {
	v, err := h.service.Usage(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, v, "获取成功")
}
