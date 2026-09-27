package handler

import (
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"emailbox/pkg/middleware"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/service"
	"emailbox/pkg/waffo"

	"github.com/labstack/echo/v5"
)

type BillingHandler struct{ service *service.BillingService }

func NewBillingHandler(s *service.BillingService) *BillingHandler { return &BillingHandler{service: s} }

// billingError 把计费错误映射成 HTTP 响应。
//
// Waffo 自身的失败回 502，但不能走 failure() 的「服务器内部错误」：用户点的是「立即订阅」，
// 他需要知道是支付服务那边的问题、稍后再试，而不是以为我们的系统坏了。原始错误照常记日志。
func billingError(c *echo.Context, err error) error {
	var apiErr *waffo.APIError
	var netErr net.Error
	switch {
	case errors.Is(err, repo.ErrNotFound):
		return failure(c, http.StatusNotFound, err)
	case errors.Is(err, service.ErrBillingDisabled), errors.Is(err, service.ErrSubscriptionExists),
		errors.Is(err, service.ErrCheckoutStale):
		return failure(c, http.StatusConflict, err)
	case errors.As(err, &apiErr), errors.As(err, &netErr):
		slog.Warn("Waffo 请求失败", "path", c.Request().URL.Path,
			"request_id", c.Response().Header().Get(echo.HeaderXRequestID), "error", err)
		return c.JSON(http.StatusBadGateway, map[string]any{
			"code": CodeFailure, "data": nil, "message": "支付服务暂时不可用，请稍后重试",
		})
	default:
		return failure(c, http.StatusBadRequest, err)
	}
}

func (h *BillingHandler) AdminSettings(c *echo.Context) error {
	v, err := h.service.Settings(c.Request().Context())
	if err != nil {
		return billingError(c, err)
	}
	return success(c, v, "获取成功")
}

func (h *BillingHandler) UpdateAdminSettings(c *echo.Context) error {
	var req model.BillingSettings
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	actor := middleware.PlatformUser(c)
	v, err := h.service.UpdateSettings(c.Request().Context(), req, actor.ID)
	if err != nil {
		return billingError(c, err)
	}
	return success(c, v, "更新成功")
}

func (h *BillingHandler) AdminPrices(c *echo.Context) error {
	items, err := h.service.ListPrices(c.Request().Context(), false)
	if err != nil {
		return billingError(c, err)
	}
	return success(c, items, "获取成功")
}

func (h *BillingHandler) CreatePrice(c *echo.Context) error {
	var req model.PlanPrice
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	v, err := h.service.CreatePrice(c.Request().Context(), req)
	if err != nil {
		return billingError(c, err)
	}
	return success(c, v, "创建成功")
}

type updatePriceRequest struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
	// 指针区分「没传」与「显式下架」。
	Active *bool `json:"active"`
}

func (h *BillingHandler) UpdatePrice(c *echo.Context) error {
	var req updatePriceRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	v, err := h.service.UpdatePrice(c.Request().Context(), c.Param("priceID"),
		strings.TrimSpace(req.Amount), strings.TrimSpace(req.Currency), req.Active)
	if err != nil {
		return billingError(c, err)
	}
	return success(c, v, "更新成功")
}

func (h *BillingHandler) SyncPrice(c *echo.Context) error {
	v, err := h.service.SyncPrice(c.Request().Context(), c.Param("priceID"))
	if err != nil {
		return billingError(c, err)
	}
	return success(c, v, "同步完成")
}

func (h *BillingHandler) Plans(c *echo.Context) error {
	items, err := h.service.ListPrices(c.Request().Context(), true)
	if err != nil {
		return billingError(c, err)
	}
	return success(c, items, "获取成功")
}

func (h *BillingHandler) Subscription(c *echo.Context) error {
	v, err := h.service.GetSubscription(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return billingError(c, err)
	}
	return success(c, v, "获取成功")
}

// Sync 向 Waffo 核对该租户未确认的结账并返回当前订阅。用量页每次打开都会调一次，
// 付完款回到页面就能看到新套餐，不必等 Webhook。
func (h *BillingHandler) Sync(c *echo.Context) error {
	sub, changed, err := h.service.SyncCheckouts(c.Request().Context(), c.Param("tenantID"))
	if err != nil {
		return billingError(c, err)
	}
	return success(c, map[string]any{"subscription": sub, "changed": changed}, "获取成功")
}

type checkoutRequest struct {
	PlanPriceID    string `json:"plan_price_id"`
	IdempotencyKey string `json:"idempotency_key"`
}

func (h *BillingHandler) Checkout(c *echo.Context) error {
	var req checkoutRequest
	if err := c.Bind(&req); err != nil {
		return failure(c, http.StatusBadRequest, err)
	}
	// 反向代理后面 Scheme() 读 X-Forwarded-Proto，Host 由代理原样转发。
	requestBase := c.Scheme() + "://" + c.Request().Host
	v, err := h.service.CreateCheckout(c.Request().Context(), c.Param("tenantID"), middleware.UserID(c),
		strings.TrimSpace(req.PlanPriceID), strings.TrimSpace(req.IdempotencyKey), requestBase)
	if err != nil {
		return billingError(c, err)
	}
	return success(c, map[string]any{"checkout_url": v.CheckoutURL, "checkout_id": v.ID, "status": v.Status}, "创建成功")
}

func (h *BillingHandler) Cancel(c *echo.Context) error {
	if err := h.service.Cancel(c.Request().Context(), c.Param("tenantID"), true); err != nil {
		return billingError(c, err)
	}
	return success(c, nil, "已请求周期末取消")
}
func (h *BillingHandler) Uncancel(c *echo.Context) error {
	if err := h.service.Cancel(c.Request().Context(), c.Param("tenantID"), false); err != nil {
		return billingError(c, err)
	}
	return success(c, nil, "已恢复订阅")
}

// Webhook 接收 Waffo 回调。
//
// 验签用原始字节：任何先解析再序列化的中间步骤都会改掉字节序列，签名随之失效。
// 返回码约定：签名/环境/Store 不对回 401；数据库等暂时故障回 500 让 Waffo 重试；
// 重试也救不回来的事件（找不到租户、价格）已记为 failed，回 200 让 Waffo 停手。
func (h *BillingHandler) Webhook(c *echo.Context) error {
	body, err := io.ReadAll(io.LimitReader(c.Request().Body, webhookBodyLimit))
	if err != nil {
		return c.NoContent(http.StatusBadRequest)
	}
	if err := h.service.VerifyWebhook(c.Request().Header.Get("X-Waffo-Signature"), body, time.Now()); err != nil {
		// 验签失败是安全事件，但请求没有可信身份可以挂进审计表；按 IP 记日志即可。
		slog.Warn("拒绝了一个 Waffo Webhook", "ip", c.RealIP(),
			"event", c.Request().Header.Get("X-Waffo-Event"), "error", err)
		return c.NoContent(http.StatusUnauthorized)
	}
	if err := h.service.ProcessWebhook(c.Request().Context(), body); err != nil {
		if errors.Is(err, service.ErrWebhookUnprocessable) {
			return c.NoContent(http.StatusOK)
		}
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.NoContent(http.StatusOK)
}

// webhookBodyLimit 是 Webhook 请求体上限。事件 JSON 只有几 KB。
const webhookBodyLimit = 1 << 20
