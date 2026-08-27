package handler

import (
	"errors"
	"net/http"

	"emailbox/pkg/mailer"
	"emailbox/pkg/model"
	"emailbox/pkg/quota"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

type GroupHandler struct{ service *service.GroupService }

func NewGroupHandler(s *service.GroupService) *GroupHandler { return &GroupHandler{service: s} }

func (h *GroupHandler) List(c *echo.Context) error {
	v, e := h.service.List(c.Request().Context(), c.Param("tenantID"))
	if e != nil {
		return failure(c, http.StatusInternalServerError, e)
	}
	return success(c, v, "获取成功")
}

func (h *GroupHandler) Create(c *echo.Context) error {
	var req model.CreateMailGroupRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, http.StatusBadRequest, e)
	}
	v, e := h.service.Create(c.Request().Context(), c.Param("tenantID"), req)
	if e != nil {
		return mailError(c, e)
	}
	return success(c, v, "创建成功")
}

func (h *GroupHandler) Update(c *echo.Context) error {
	var req model.UpdateMailGroupRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, http.StatusBadRequest, e)
	}
	v, e := h.service.Update(c.Request().Context(), c.Param("tenantID"), c.Param("groupID"), req)
	if e != nil {
		return mailError(c, e)
	}
	return success(c, v, "更新成功")
}

func (h *GroupHandler) Reorder(c *echo.Context) error {
	var req model.ReorderMailGroupsRequest
	if e := c.Bind(&req); e != nil {
		return failure(c, http.StatusBadRequest, e)
	}
	if e := h.service.Reorder(c.Request().Context(), c.Param("tenantID"), req.GroupIDs); e != nil {
		return mailError(c, e)
	}
	return success(c, nil, "排序成功")
}

func (h *GroupHandler) Delete(c *echo.Context) error {
	if e := h.service.Delete(c.Request().Context(), c.Param("tenantID"), c.Param("groupID")); e != nil {
		return mailError(c, e)
	}
	return success(c, nil, "删除成功")
}

// mailError 把邮箱业务的领域错误映射到统一的状态码与业务码。
//
// 跨租户访问会在 repo 层因为「WHERE 带 tenant_id 却更新了 0 行」而变成 ErrNotFound，
// 这里统一回 404——不能回 403，否则响应本身就泄露了「该 ID 确实存在」。
func mailError(c *echo.Context, err error) error {
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return failure(c, http.StatusNotFound, err)
	case errors.Is(err, repo.ErrConflict):
		return failure(c, http.StatusConflict, err)
	case errors.Is(err, quota.ErrQuotaExceeded):
		return c.JSON(http.StatusForbidden, map[string]any{
			"code": CodeQuotaExceeded, "data": nil, "message": err.Error(),
		})
	// 被封/停用的账号是 409：请求本身没错，是这个账号当前不能用。
	// 用 400 的话前端分不清「参数写错了」和「换个账号就好了」。
	case errors.Is(err, service.ErrAccountBanned), errors.Is(err, service.ErrAccountDisabled):
		return failure(c, http.StatusConflict, err)
	case isUpstreamError(err):
		return upstreamFailure(c, err)
	default:
		return failure(c, http.StatusBadRequest, err)
	}
}

// isUpstreamError 判断错误是否来自邮件协议层。
func isUpstreamError(err error) bool {
	var e *mailer.Error
	return errors.As(err, &e)
}

// upstreamFailure 把协议层的结构化错误映射成 HTTP 响应。
//
// error_kind 一并回传：前端据此区分「重新授权」「联系服务商」「检查代理」这几类
// 完全不同的处置方式，只给一段文案的话用户不知道该做什么。
//
// **这里绝不能返回 401。** 401 的含义是「本次请求的调用方没有通过认证」，
// 而这些错误说的是「调用方没问题，是他托管的那个邮箱的凭据过期了」——两件事。
// 曾经把 auth_failed / consent_required 映射成 401，后果是：
// 用户导入的一批账号里只要有一个 token 失效，点开它，前端拦截器就把 401 当成
// 会话过期，清掉会话、把**用户本人**踢回登录页，而他完全不知道发生了什么。
// 对外 API 的调用方会踩得更深：它们会以为自己的 API 令牌废了，跑去重新签发。
//
// 这两类因此落到默认的 502（也和 05 文档 §1.2 的契约一致：1005 → 502）。
// 具体是「要重新授权」还是「要换代理」，调用方读 data.error_kind，不靠状态码区分。
func upstreamFailure(c *echo.Context, err error) error {
	var e *mailer.Error
	errors.As(err, &e)

	status := http.StatusBadGateway
	switch e.Kind {
	case mailer.ErrKindBanned:
		status = http.StatusConflict
	case mailer.ErrKindRateLimited:
		status = http.StatusTooManyRequests
	case mailer.ErrKindFolderUnavailable:
		status = http.StatusNotFound
	case mailer.ErrKindCanceled:
		status = http.StatusGatewayTimeout
	}
	return c.JSON(status, map[string]any{
		"code": CodeUpstreamMailErr,
		"data": map[string]any{"error_kind": string(e.Kind), "channel": e.Channel},
		// Message 是面向用户的中文文案；Detail 可能含服务器返回的原文，不外泄。
		"message": e.Message,
	})
}
