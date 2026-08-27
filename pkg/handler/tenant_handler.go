package handler

import (
	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

type TenantHandler struct{ service *service.TenantService }

func NewTenantHandler(s *service.TenantService) *TenantHandler { return &TenantHandler{service: s} }
func (h *TenantHandler) List(c *echo.Context) error {
	v, e := h.service.List(c.Request().Context(), middleware.UserID(c))
	if e != nil {
		return failure(c, 500, e)
	}
	return success(c, v, "获取成功")
}
func (h *TenantHandler) Create(c *echo.Context) error {
	var req model.CreateTenantRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, 400, e)
	}
	v, e := h.service.Create(c.Request().Context(), middleware.UserID(c), req)
	if e != nil {
		return failure(c, 400, e)
	}
	return success(c, v, "创建成功")
}
func (h *TenantHandler) Get(c *echo.Context) error {
	v, e := h.service.Get(c.Request().Context(), c.Param("tenantID"))
	if e != nil {
		return failure(c, 404, e)
	}
	return success(c, v, "获取成功")
}
func (h *TenantHandler) Update(c *echo.Context) error {
	var req model.UpdateTenantRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, 400, e)
	}
	v, e := h.service.Update(c.Request().Context(), c.Param("tenantID"), req)
	if e != nil {
		return failure(c, 400, e)
	}
	return success(c, v, "更新成功")
}
func (h *TenantHandler) Delete(c *echo.Context) error {
	if e := h.service.Delete(c.Request().Context(), c.Param("tenantID")); e != nil {
		return failure(c, 400, e)
	}
	return success(c, nil, "删除成功")
}
func (h *TenantHandler) Select(c *echo.Context) error {
	session := middleware.Session(c)
	if e := h.service.Select(c.Request().Context(), session.ID, middleware.UserID(c), c.Param("tenantID")); e != nil {
		return failure(c, 400, e)
	}
	return success(c, nil, "切换成功")
}
