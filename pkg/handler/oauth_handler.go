package handler

import (
	"errors"
	"net/http"
	"net/url"

	"emailbox/pkg/middleware"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"

	"github.com/labstack/echo/v5"
)

type OAuthHandler struct {
	service   *service.OAuthService
	returnURL string
}

func NewOAuthHandler(s *service.OAuthService, returnURL string) *OAuthHandler {
	return &OAuthHandler{service: s, returnURL: returnURL}
}

func (h *OAuthHandler) Start(c *echo.Context) error {
	result, err := h.service.Start(c.Request().Context(), c.Param("tenantID"), c.Param("accountID"), middleware.UserID(c))
	if err != nil {
		return mailError(c, err)
	}
	return success(c, result, "授权流程已创建")
}

type completeOAuthRequest struct {
	FlowID        string `json:"flow_id"`
	RedirectedURL string `json:"redirected_url"`
}

func (h *OAuthHandler) Complete(c *echo.Context) error {
	var req completeOAuthRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	tenantID, accountID := c.Param("tenantID"), c.Param("accountID")
	if req.RedirectedURL != "" {
		exchanged, err := h.service.ExchangeRedirectedURL(c.Request().Context(), req.RedirectedURL)
		if err != nil {
			return mailError(c, err)
		}
		if exchanged.TenantID != tenantID || exchanged.AccountID != accountID || (req.FlowID != "" && exchanged.FlowID != req.FlowID) {
			return mailError(c, service.ErrOAuthFlowInvalid)
		}
		req.FlowID = exchanged.FlowID
	}
	if req.FlowID == "" {
		return failure(c, http.StatusBadRequest, errors.New("缺少 flow_id"))
	}
	result, err := h.service.Complete(c.Request().Context(), tenantID, accountID, middleware.UserID(c), req.FlowID)
	if err != nil {
		return mailError(c, err)
	}
	return success(c, result, "重新授权成功")
}

// Callback 是公开端点。state 自带一次性随机量并绑定租户、账号和发起用户；
// 这里完成交换后只把 flow_id 带回前端，令牌始终留在服务端。
func (h *OAuthHandler) Callback(c *echo.Context) error {
	q := c.Request().URL.Query()
	result, err := h.service.ExchangeCallback(c.Request().Context(), q.Get("state"), q.Get("code"), q.Get("error_description"))
	returnTo, parseErr := url.Parse(h.returnURL)
	if parseErr != nil {
		return failure(c, http.StatusInternalServerError, parseErr)
	}
	values := returnTo.Query()
	if err != nil {
		values.Set("oauth_error", publicOAuthError(err))
	} else {
		values.Set("oauth_status", "ready")
		values.Set("oauth_flow_id", result.FlowID)
		values.Set("oauth_tenant_id", result.TenantID)
		values.Set("oauth_account_id", result.AccountID)
	}
	returnTo.RawQuery = values.Encode()
	return c.Redirect(http.StatusFound, returnTo.String())
}

func publicOAuthError(err error) string {
	if errors.Is(err, service.ErrOAuthIdentityMismatch) {
		return err.Error()
	}
	if errors.Is(err, service.ErrOAuthFlowInvalid) || errors.Is(err, repo.ErrNotFound) {
		return service.ErrOAuthFlowInvalid.Error()
	}
	return "微软授权未完成，请重新发起"
}
