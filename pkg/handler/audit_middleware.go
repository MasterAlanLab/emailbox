package handler

import (
	"net/http"

	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

// AuditWrite 记录一次写操作。用户侧与管理员侧挂的是同一份路由表，
// actor_kind 由中间件从 context 取，因此同一条注册在两边分别产出
// actor_kind='user' 与 'admin' 的记录，不需要写两遍。
//
// idParam 是资源 ID 在路径里的参数名，没有就传空串（例如批量接口）。
func AuditWrite(audit *service.AuditService, action, resourceType, idParam string) echo.MiddlewareFunc {
	return auditing(audit, action, resourceType, idParam, false)
}

// AuditAdminRead 记录管理员的读操作。
//
// 只记管理员：普通用户翻十页邮件就是十条记录，量大到会把真正要看的写操作淹掉
// （08 文档 §2.4）。必须记的是「管理员看了别人的什么」——那是本平台
// 最需要事后追溯的一类行为，而它在业务上不留任何痕迹。
func AuditAdminRead(audit *service.AuditService, action, resourceType, idParam string) echo.MiddlewareFunc {
	return auditing(audit, action, resourceType, idParam, true)
}

func auditing(audit *service.AuditService, action, resourceType, idParam string, adminOnly bool) echo.MiddlewareFunc {
	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c *echo.Context) error {
			err := next(c)

			if adminOnly && middleware.ActorKind(c) != model.ActorKindAdmin {
				return err
			}
			// 失败的操作不记：一次被配额拦下的创建没有改变任何东西，
			// 记下来只会让审计里全是噪音。错误本身由访问日志覆盖。
			//
			// handler 返回 nil 但状态是 4xx 是常见形态（failure() 就是这么写的），
			// 所以光看 err 不够，还要问一次实际写出去的状态码。
			if err != nil || !wroteSuccess(c) {
				return err
			}

			resourceID := ""
			if idParam != "" {
				resourceID = c.Param(idParam)
			}
			actorID, actorEmail := actorIdentity(c)

			audit.Record(c.Request().Context(), service.Entry{
				TenantID:     c.Param("tenantID"),
				ActorUserID:  actorID,
				ActorName:    actorEmail,
				ActorKind:    middleware.ActorKind(c),
				Action:       action,
				ResourceType: resourceType,
				ResourceID:   resourceID,
				IP:           c.RealIP(),
			})
			return err
		}
	}
}

// wroteSuccess 判断这次请求最终写出去的是不是 2xx/3xx。
// Echo v5 的 Response() 静态类型是 http.ResponseWriter，状态码要断言回 *echo.Response 才拿得到；
// 断言失败时保守地当作成功，宁可多记一条也不要因为换了个 ResponseWriter 实现就静默丢审计。
func wroteSuccess(c *echo.Context) bool {
	resp, ok := c.Response().(*echo.Response)
	if !ok {
		return true
	}
	return resp.Status < http.StatusBadRequest
}

// actorIdentity 取操作者。管理员走 RequirePlatformAdmin 时 context 里有完整用户，
// 普通用户只有 session 带来的 user_id——邮箱留空，查询时按 id 关联即可。
// actorIdentity 返回操作者的 ID 与用户名。用户名而不是邮箱：
// 邮箱自 000008 起可以不填，用它做「用户被删后仍能追溯是谁」这层兜底，
// 会在最需要追溯的时候正好是空的。
func actorIdentity(c *echo.Context) (id, name string) {
	if u := middleware.PlatformUser(c); u != nil {
		return u.ID, u.Username
	}
	return middleware.UserID(c), ""
}
