package handler

import (
	"errors"
	"net/http"
	"strings"

	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

// AdminHandler 提供 /api/v1/admin 下的全部端点。
// 跨租户的邮箱管理**不在这里**——那些直接复用用户侧的 handler，
// 差别只在 tenantID 从哪条路径来（见 api/routes.go 的 mountMailRoutes）。
type AdminHandler struct {
	service *service.AdminService
}

func NewAdminHandler(s *service.AdminService) *AdminHandler {
	return &AdminHandler{service: s}
}

// adminError 把管理端的领域错误映射成 HTTP 状态。
func adminError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return failure(c, http.StatusNotFound, err)
	case errors.Is(err, repo.ErrConflict):
		return failure(c, http.StatusConflict, err)
	// 这三类都是「请求合法但当前状态不允许」，用 409 而不是 400：
	// 前端据此知道该提示用户改变状态，而不是提示他改参数。
	case errors.Is(err, service.ErrLastAdmin),
		errors.Is(err, service.ErrSelfTarget),
		errors.Is(err, service.ErrPlanInUse),
		errors.Is(err, service.ErrPlanIsDefault):
		return failure(c, http.StatusConflict, err)
	default:
		return failure(c, http.StatusBadRequest, err)
	}
}

// ---------- 用户 ----------

func (h *AdminHandler) ListUsers(c *echo.Context) error {
	q := c.Request().URL.Query()
	filter := model.AdminUserFilter{
		Query:        strings.TrimSpace(q.Get("q")),
		Status:       q.Get("status"),
		PlatformRole: q.Get("platform_role"),
		Page:         atoiOrZero(q.Get("page")),
		Limit:        atoiOrZero(q.Get("limit")),
	}
	filter.Normalize()

	items, total, err := h.service.ListUsers(c.Request().Context(), filter)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, map[string]any{
		"items":      items,
		"pagination": pagination(filter.Page, filter.Limit, total),
	}, "获取成功")
}

func (h *AdminHandler) GetUser(c *echo.Context) error {
	v, err := h.service.GetUser(c.Request().Context(), c.Param("userID"))
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "获取成功")
}

type updateUserRequest struct {
	Status       *string `json:"status"`
	PlatformRole *string `json:"platform_role"`
}

func (h *AdminHandler) UpdateUser(c *echo.Context) error {
	var req updateUserRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}

	update := service.UserUpdate{}
	if req.Status != nil {
		status := model.UserStatus(*req.Status)
		if status != model.UserStatusActive && status != model.UserStatusDisabled {
			return failure(c, http.StatusBadRequest, errors.New("状态只能是 active 或 disabled"))
		}
		update.Status = &status
	}
	if req.PlatformRole != nil {
		role := model.PlatformRole(*req.PlatformRole)
		if role != model.PlatformRoleUser && role != model.PlatformRoleAdmin {
			return failure(c, http.StatusBadRequest, errors.New("平台角色只能是 user 或 admin"))
		}
		update.PlatformRole = &role
	}

	actor := middleware.PlatformUser(c)
	userID := c.Param("userID")
	if err := h.service.UpdateUser(c.Request().Context(), actor.ID, userID, update); err != nil {
		return adminError(c, err)
	}

	v, err := h.service.GetUser(c.Request().Context(), userID)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "更新成功")
}

func (h *AdminHandler) ResetPassword(c *echo.Context) error {
	password, err := h.service.ResetPassword(c.Request().Context(), c.Param("userID"))
	if err != nil {
		return adminError(c, err)
	}
	return success(c, map[string]any{"password": password},
		"已重置，请把临时密码转交给用户；本次之后无法再次查看")
}

func (h *AdminHandler) DeleteUser(c *echo.Context) error {
	accounts, err := h.service.DeleteUser(c.Request().Context(), middleware.PlatformUser(c).ID, c.Param("userID"))
	if err != nil {
		return adminError(c, err)
	}
	return success(c, map[string]any{"deleted_accounts": accounts}, "删除成功")
}

// ---------- 套餐 ----------

func (h *AdminHandler) ListPlans(c *echo.Context) error {
	items, err := h.service.ListPlans(c.Request().Context())
	if err != nil {
		return adminError(c, err)
	}
	return success(c, items, "获取成功")
}

func (h *AdminHandler) CreatePlan(c *echo.Context) error {
	var plan model.Plan
	if err := c.Bind(&plan); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	created, err := h.service.CreatePlan(c.Request().Context(), plan)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, created, "创建成功")
}

func (h *AdminHandler) UpdatePlan(c *echo.Context) error {
	var plan model.Plan
	if err := c.Bind(&plan); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	plan.ID = c.Param("planID")
	updated, err := h.service.UpdatePlan(c.Request().Context(), plan)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, updated, "更新成功")
}

func (h *AdminHandler) DeletePlan(c *echo.Context) error {
	if err := h.service.DeletePlan(c.Request().Context(), c.Param("planID")); err != nil {
		return adminError(c, err)
	}
	return success(c, nil, "删除成功")
}

// ---------- 配额 ----------

func (h *AdminHandler) GetTenantPlan(c *echo.Context) error {
	v, err := h.service.TenantPlan(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "获取成功")
}

type assignPlanRequest struct {
	PlanID string `json:"plan_id"`
}

// AssignTenantPlan 直接给用户分配套餐，不经支付。
func (h *AdminHandler) AssignTenantPlan(c *echo.Context) error {
	var req assignPlanRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	if strings.TrimSpace(req.PlanID) == "" {
		return failure(c, http.StatusBadRequest, errors.New("请选择要分配的套餐"))
	}
	tenantID := c.Param("tenantID")
	if err := h.service.AssignPlan(c.Request().Context(), tenantID, req.PlanID); err != nil {
		return adminError(c, err)
	}
	v, err := h.service.TenantPlan(c.Request().Context(), tenantID)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "已分配套餐")
}

// ---------- 概览 ----------

func (h *AdminHandler) Stats(c *echo.Context) error {
	v, err := h.service.Stats(c.Request().Context())
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "获取成功")
}

// pagination 拼分页元信息，字段名与 model.Pagination 保持一致。
func pagination(page, limit, total int) map[string]any {
	pages := 0
	if limit > 0 {
		pages = (total + limit - 1) / limit
	}
	return map[string]any{"page": page, "limit": limit, "total": total, "pages": pages}
}
