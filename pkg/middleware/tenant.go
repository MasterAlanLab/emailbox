package middleware

import (
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"net/http"

	"github.com/labstack/echo/v5"
)

type TenantMiddleware struct{ store *repo.Store }

func NewTenantMiddleware(s *repo.Store) *TenantMiddleware { return &TenantMiddleware{store: s} }
func (m *TenantMiddleware) Member(next echo.HandlerFunc) echo.HandlerFunc {
	return func(c *echo.Context) error {
		// API Key 不是成员，也不该是：它不属于任何用户，给它建一行 tenant_members
		// 等于让「谁在这个工作空间里」这份名单多出一个不是人的条目。
		// 它绑定的租户在鉴权时就定了，这里只校验 URL 里的 tenantID 与之一致，
		// 然后交出一个只读的虚拟成员——下游的 Require 照常判权限。
		if keyTenant := APIKeyTenant(c); keyTenant != "" {
			if c.Param("tenantID") != keyTenant {
				return echo.NewHTTPError(http.StatusForbidden, "API Key 无权访问该工作空间")
			}
			c.Set("tenant_member", &model.TenantMember{TenantID: keyTenant, Role: model.TenantRoleAPI})
			return next(c)
		}
		member, e := m.store.GetMember(c.Request().Context(), c.Param("tenantID"), UserID(c))
		if e != nil {
			return echo.NewHTTPError(http.StatusForbidden, "您不是该租户成员")
		}
		c.Set("tenant_member", member)
		return next(c)
	}
}

// Require 校验当前用户在该租户内是否具备某项权限。
//
// 平台管理员直接放行：管理员访问的是**别人的**租户，按定义不会是其成员，
// 走成员判断必然 403。这个放行只可能在 RequirePlatformAdmin 已经跑过之后触发
// （platform_user 只有它会设），因此它不是一个可以被绕开的旁路。
//
// 注意放行的只是「谁有权决定 tenant_id 的取值」这一层。下游 service 与 repo
// 的 SQL 仍然照常带 WHERE tenant_id = ?，隔离规则永不放宽（08 文档 §2.3）。
func Require(permission model.Permission) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			if PlatformUser(c) != nil {
				return next(c)
			}
			member, ok := c.Get("tenant_member").(*model.TenantMember)
			if !ok || member == nil || !model.HasPermission(member.Role, permission) {
				return echo.NewHTTPError(http.StatusForbidden, "权限不足")
			}
			return next(c)
		}
	}
}
func TenantMember(c *echo.Context) *model.TenantMember {
	v, ok := c.Get("tenant_member").(*model.TenantMember)
	if !ok {
		return nil
	}
	return v
}
