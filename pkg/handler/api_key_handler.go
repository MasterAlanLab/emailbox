package handler

import (
	"net/http"

	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

type APIKeyHandler struct{ service *service.APIKeyService }

func NewAPIKeyHandler(s *service.APIKeyService) *APIKeyHandler { return &APIKeyHandler{service: s} }

// Get 回显当前 Key。还没生成时 data 为 null，页面据此显示「生成」而不是「重置」。
func (h *APIKeyHandler) Get(c *echo.Context) error {
	v, err := h.service.Get(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	return success(c, v, "获取成功")
}

// Reset 生成新 Key 并覆盖旧的。这两个端点都要 tenant:update 权限，
// 而 API Key 自己的角色没有这一项——它读不到也重置不了自己。
func (h *APIKeyHandler) Reset(c *echo.Context) error {
	v, err := h.service.Reset(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return failure(c, http.StatusInternalServerError, err)
	}
	return success(c, v, "已生成新的 API Key")
}
