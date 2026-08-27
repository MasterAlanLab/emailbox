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
	audit   *service.AuditService
	quota   *service.QuotaService
}

func NewAdminHandler(s *service.AdminService, audit *service.AuditService, q *service.QuotaService) *AdminHandler {
	return &AdminHandler{service: s, audit: audit, quota: q}
}

// adminError 把管理后台的领域错误映射成 HTTP 状态。
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
	changed, err := h.service.UpdateUser(c.Request().Context(), actor.ID, userID, update)
	if err != nil {
		return adminError(c, err)
	}

	// 只在真的改了东西时记审计：一次「打开详情页顺手保存」不该在日志里
	// 留下一条什么都没变的记录，那会把真正的变更淹掉。
	if len(changed) > 0 {
		details := map[string]any{"changed": changed}
		if update.Status != nil {
			details["status"] = string(*update.Status)
		}
		if update.PlatformRole != nil {
			details["platform_role"] = string(*update.PlatformRole)
		}
		h.recordUserAudit(c, model.AuditUserUpdate, userID, details)
	}

	v, err := h.service.GetUser(c.Request().Context(), userID)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "更新成功")
}

func (h *AdminHandler) ResetPassword(c *echo.Context) error {
	userID := c.Param("userID")
	password, err := h.service.ResetPassword(c.Request().Context(), userID)
	if err != nil {
		return adminError(c, err)
	}
	// 审计只记「重置了谁的密码」，绝不记密码本身——
	// 审计表是给人看的，把明文密码写进去等于给它开了个新的泄露面。
	h.recordUserAudit(c, model.AuditUserResetPassword, userID, nil)
	return success(c, map[string]any{"password": password},
		"已重置，请把临时密码转交给用户；本次之后无法再次查看")
}

func (h *AdminHandler) DeleteUser(c *echo.Context) error {
	userID := c.Param("userID")
	accounts, err := h.service.DeleteUser(c.Request().Context(), middleware.PlatformUser(c).ID, userID)
	if err != nil {
		return adminError(c, err)
	}
	h.recordUserAudit(c, model.AuditUserDelete, userID, map[string]any{
		"deleted_accounts": accounts,
	})
	return success(c, map[string]any{"deleted_accounts": accounts}, "删除成功")
}

// recordUserAudit 记一条以「用户」为资源的审计。
//
// 这类操作发生在平台层面而不是某个租户里，tenant_id 因此填被操作者的个人空间；
// 取不到就留空——审计表的 tenant_id 是 NOT NULL，但空串比因为一次取不到租户
// 就整条丢掉要好。
func (h *AdminHandler) recordUserAudit(c *echo.Context, action, userID string, details map[string]any) {
	actor := middleware.PlatformUser(c)
	tenantID := ""
	if target, err := h.service.GetUser(c.Request().Context(), userID); err == nil {
		tenantID = target.TenantID
	}
	h.audit.Record(c.Request().Context(), service.Entry{
		TenantID:     tenantID,
		ActorUserID:  actor.ID,
		ActorName:    actor.Username,
		ActorKind:    middleware.ActorKind(c),
		Action:       action,
		ResourceType: "user",
		ResourceID:   userID,
		IP:           c.RealIP(),
		Details:      details,
	})
}

// ---------- 租户 ----------

func (h *AdminHandler) ListTenants(c *echo.Context) error {
	q := c.Request().URL.Query()
	filter := model.AdminTenantFilter{
		Query: strings.TrimSpace(q.Get("q")),
		Page:  atoiOrZero(q.Get("page")),
		Limit: atoiOrZero(q.Get("limit")),
	}
	filter.Normalize()

	items, total, err := h.service.ListTenants(c.Request().Context(), filter)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, map[string]any{
		"items":      items,
		"pagination": pagination(filter.Page, filter.Limit, total),
	}, "获取成功")
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
	h.recordPlanAudit(c, model.AuditPlanCreate, created.ID, map[string]any{"code": created.Code})
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
	h.recordPlanAudit(c, model.AuditPlanUpdate, updated.ID, map[string]any{"code": updated.Code})
	return success(c, updated, "更新成功")
}

func (h *AdminHandler) DeletePlan(c *echo.Context) error {
	planID := c.Param("planID")
	if err := h.service.DeletePlan(c.Request().Context(), planID); err != nil {
		return adminError(c, err)
	}
	h.recordPlanAudit(c, model.AuditPlanDelete, planID, nil)
	return success(c, nil, "删除成功")
}

func (h *AdminHandler) recordPlanAudit(c *echo.Context, action, planID string, details map[string]any) {
	actor := middleware.PlatformUser(c)
	h.audit.Record(c.Request().Context(), service.Entry{
		ActorUserID: actor.ID, ActorName: actor.Username, ActorKind: middleware.ActorKind(c),
		Action: action, ResourceType: "plan", ResourceID: planID,
		IP: c.RealIP(), Details: details,
	})
}

// ---------- 配额 ----------

func (h *AdminHandler) GetTenantQuota(c *echo.Context) error {
	v, err := h.quota.Usage(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "获取成功")
}

type updateQuotaRequest struct {
	PlanID            string `json:"plan_id"`
	Note              string `json:"note"`
	MaxAccounts       *int   `json:"max_accounts"`
	MaxGroups         *int   `json:"max_groups"`
	DailyMailFetch    *int   `json:"daily_mail_fetch"`
	DailyTokenRefresh *int   `json:"daily_token_refresh"`
}

func (h *AdminHandler) UpdateTenantQuota(c *echo.Context) error {
	var req updateQuotaRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	tenantID := c.Param("tenantID")
	actor := middleware.PlatformUser(c)

	err := h.service.UpdateTenantQuota(c.Request().Context(), tenantID, actor.ID, service.QuotaUpdate{
		PlanID: req.PlanID,
		Overrides: repo.QuotaOverrides{
			MaxAccounts:       req.MaxAccounts,
			MaxGroups:         req.MaxGroups,
			DailyMailFetch:    req.DailyMailFetch,
			DailyTokenRefresh: req.DailyTokenRefresh,
			Note:              req.Note,
		},
	})
	if err != nil {
		return adminError(c, err)
	}

	h.audit.Record(c.Request().Context(), service.Entry{
		TenantID: tenantID, ActorUserID: actor.ID, ActorName: actor.Username,
		ActorKind: middleware.ActorKind(c), Action: model.AuditQuotaUpdate,
		ResourceType: "tenant", ResourceID: tenantID, IP: c.RealIP(),
		// note 是调额原因，正是三个月后回看时唯一有用的信息，必须进 details。
		Details: map[string]any{"note": req.Note, "plan_id": req.PlanID},
	})

	v, err := h.quota.Usage(c.Request().Context(), tenantID)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, v, "更新成功")
}

// ---------- 审计与概览 ----------

func (h *AdminHandler) ListAudit(c *echo.Context) error {
	q := c.Request().URL.Query()
	filter := model.AuditFilter{
		TenantID:    q.Get("tenant_id"),
		ActorUserID: q.Get("actor_user_id"),
		ActorKind:   q.Get("actor_kind"),
		Action:      q.Get("action"),
		Page:        atoiOrZero(q.Get("page")),
		Limit:       atoiOrZero(q.Get("limit")),
	}
	filter.Normalize()

	items, total, err := h.audit.List(c.Request().Context(), filter)
	if err != nil {
		return adminError(c, err)
	}
	return success(c, map[string]any{
		"items":      items,
		"pagination": pagination(filter.Page, filter.Limit, total),
	}, "获取成功")
}

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
