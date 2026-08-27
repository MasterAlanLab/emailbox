package handler

import (
	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

type MemberHandler struct{ service *service.MemberService }

func NewMemberHandler(s *service.MemberService) *MemberHandler { return &MemberHandler{service: s} }
func (h *MemberHandler) List(c *echo.Context) error {
	v, e := h.service.List(c.Request().Context(), c.Param("tenantID"))
	if e != nil {
		return failure(c, 500, e)
	}
	return success(c, v, "获取成功")
}
func (h *MemberHandler) Add(c *echo.Context) error {
	var req model.AddMemberRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, 400, e)
	}
	v, e := h.service.Add(c.Request().Context(), c.Param("tenantID"), *middleware.TenantMember(c), req)
	if e != nil {
		return failure(c, 400, e)
	}
	return success(c, v, "添加成功")
}
func (h *MemberHandler) Update(c *echo.Context) error {
	var req model.UpdateMemberRoleRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, 400, e)
	}
	e := h.service.UpdateRole(c.Request().Context(), c.Param("tenantID"), c.Param("userID"), *middleware.TenantMember(c), req.Role)
	if e != nil {
		return failure(c, 400, e)
	}
	return success(c, nil, "更新成功")
}
func (h *MemberHandler) Delete(c *echo.Context) error {
	e := h.service.Delete(c.Request().Context(), c.Param("tenantID"), c.Param("userID"), *middleware.TenantMember(c))
	if e != nil {
		return failure(c, 400, e)
	}
	return success(c, nil, "删除成功")
}
