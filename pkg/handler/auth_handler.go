package handler

import (
	"emailbox/configs"
	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/service"
	"net/http"
	"time"

	"github.com/labstack/echo/v5"
)

type AuthHandler struct {
	service *service.AuthService
	// desktop 是进程的形态，不是某个用户的状态，所以停在处理器这一层：
	// AuthService 负责的是「这把令牌属于谁」，与进程有没有窗口无关。
	desktop bool
}

func NewAuthHandler(s *service.AuthService, desktop bool) *AuthHandler {
	return &AuthHandler{service: s, desktop: desktop}
}

// respondAuth 统一下发认证响应，顺带盖上形态标志。
//
// 三个入口都走它，而不是只在 Session 里盖：桌面版确实只经过 Session
// （自动登录不走登录框），但「只有一条路径记得盖章」这种约定，
// 在下一次有人给前端加一个登录后跳转时就会失效。
func (h *AuthHandler) respondAuth(c *echo.Context, v *model.AuthResponse, msg string) error {
	v.Desktop = h.desktop
	return success(c, v, msg)
}

// SetSessionCookie 写入会话 Cookie。
//
// 导出是因为桌面版的自动登录入口要写出与网页登录**完全一样**的 Cookie。
// 属性（HttpOnly / Secure / SameSite / MaxAge）抄第二遍迟早会漂移，
// 而漂移的方向通常是「桌面版那份少了 HttpOnly」这类没人会立刻发现的。
func SetSessionCookie(c *echo.Context, token string) {
	c.SetCookie(&http.Cookie{Name: middleware.SessionCookie, Value: token, Path: "/", MaxAge: configs.AppConfig.Session.ExpireHour * 3600, HttpOnly: true, Secure: configs.AppConfig.Session.CookieSecure, SameSite: http.SameSiteLaxMode})
}

func (h *AuthHandler) setCookie(c *echo.Context, token string) { SetSessionCookie(c, token) }
func (h *AuthHandler) Register(c *echo.Context) error {
	var req model.RegisterRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, 400, e)
	}
	v, t, e := h.service.Register(c.Request().Context(), req)
	if e != nil {
		return failure(c, 400, e)
	}
	h.setCookie(c, t)
	return h.respondAuth(c, v, "注册成功")
}
func (h *AuthHandler) Login(c *echo.Context) error {
	var req model.LoginRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, 400, e)
	}
	v, t, e := h.service.Login(c.Request().Context(), req)
	if e != nil {
		return failure(c, 401, e)
	}
	h.setCookie(c, t)
	return h.respondAuth(c, v, "登录成功")
}
func (h *AuthHandler) Logout(c *echo.Context) error {
	if cookie, e := c.Cookie(middleware.SessionCookie); e == nil {
		if e := h.service.Logout(c.Request().Context(), cookie.Value); e != nil {
			return failure(c, http.StatusInternalServerError, e)
		}
	}
	c.SetCookie(&http.Cookie{Name: middleware.SessionCookie, Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(0, 0), HttpOnly: true, Secure: configs.AppConfig.Session.CookieSecure, SameSite: http.SameSiteLaxMode})
	return success(c, nil, "退出成功")
}
func (h *AuthHandler) Session(c *echo.Context) error {
	cookie, e := c.Cookie(middleware.SessionCookie)
	if e != nil {
		return failure(c, 401, e)
	}
	v, _, e := h.service.Session(c.Request().Context(), cookie.Value)
	if e != nil {
		return failure(c, 401, e)
	}
	return h.respondAuth(c, v, "获取成功")
}
