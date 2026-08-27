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

type AuthHandler struct{ service *service.AuthService }

func NewAuthHandler(s *service.AuthService) *AuthHandler { return &AuthHandler{service: s} }
func (h *AuthHandler) setCookie(c *echo.Context, token string) {
	c.SetCookie(&http.Cookie{Name: middleware.SessionCookie, Value: token, Path: "/", MaxAge: configs.AppConfig.Session.ExpireHour * 3600, HttpOnly: true, Secure: configs.AppConfig.Session.CookieSecure, SameSite: http.SameSiteLaxMode})
}
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
	return success(c, v, "注册成功")
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
	return success(c, v, "登录成功")
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
	return success(c, v, "获取成功")
}
