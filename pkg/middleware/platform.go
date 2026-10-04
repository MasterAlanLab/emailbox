package middleware

import (
	"net/http"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"

	"github.com/labstack/echo/v5"
)

// PlatformMiddleware 校验平台角色（users.platform_role），与租户角色正交。
type PlatformMiddleware struct{ store *repo.Store }

func NewPlatformMiddleware(s *repo.Store) *PlatformMiddleware {
	return &PlatformMiddleware{store: s}
}

// RequirePlatformAdmin 只放行平台管理员。
//
// 注意：本中间件只决定「谁有权指定 tenant_id」。下游 service 与 repo 的
// SQL 仍然照常带 `WHERE tenant_id = ?`，与普通用户走完全相同的代码路径，
// 隔离规则永不放宽。
func (m *PlatformMiddleware) RequirePlatformAdmin(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		userID := UserID(c)
		if userID == "" {
			return echo.NewHTTPError(http.StatusUnauthorized, "用户未认证")
		}
		user, err := m.store.GetUserByID(c.Request().Context(), userID)
		// 用户被删或被禁用时，即使 platform_role 仍是 admin 也一律拒绝。
		if err != nil || user.Status != model.UserStatusActive || !user.IsPlatformAdmin() {
			return echo.NewHTTPError(http.StatusForbidden, "需要平台管理员权限")
		}
		c.Set("platform_user", user)
		return next(c)
	}
}

// TenantContext 校验 URL 上的租户确实存在。
//
// 不校验的话，管理员传一个不存在的 tenantID 会一路走到 service，
// 拿到一个空列表——看起来像「这个租户没有邮箱」，实际是租户 ID 打错了。
// 这两种情况在排障时的处置完全不同，必须分开。
//
// 这里原本还会打一个 X-Admin-Context 响应头供前端显示 Banner，已删除：
// 前端从来没读过它，Banner 是按路由渲染的（AdminTenantMailPage），
// 而管理员身份本身来自会话里的 platform_role，不需要逐个响应重复声明。
func (m *PlatformMiddleware) TenantContext(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		tenantID := c.Param("tenantID")
		if _, err := m.store.GetTenantByID(c.Request().Context(), tenantID); err != nil {
			return echo.NewHTTPError(http.StatusNotFound, "租户不存在")
		}
		return next(c)
	}
}

// PlatformUser 返回当前请求的平台管理员，未经过 RequirePlatformAdmin 时为 nil。
func PlatformUser(c *echo.Context) *model.User {
	v, ok := c.Get("platform_user").(*model.User)
	if !ok {
		return nil
	}
	return v
}
