package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"time"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/waffo"

	"github.com/google/uuid"
)

// checkoutTTL 是结账会话的有效期。比 Waffo 默认的 45 分钟短：会话锁定了价格，
// 管理员调价后旧会话还能按旧价付款，这个窗口越短越好。
const checkoutTTL = 30 * time.Minute

// 仍占着「当前订阅」位置的状态。这些状态下再买一份会变成两份扣款。
var blockingSubscriptionStatus = map[string]bool{"pending": true, "active": true, "canceling": true, "past_due": true}

func (s *BillingService) GetSubscription(ctx context.Context, tenantID string) (*model.Subscription, error) {
	v, err := s.store.GetSubscription(ctx, tenantID)
	if errors.Is(err, repo.ErrNotFound) {
		return nil, nil
	}
	return v, err
}

// CreateCheckout 为租户创建一次 Waffo 结账，返回带顾客令牌的一次性地址。
//
// 采用官方 SDK 的 authenticated checkout 做法：商户签名建会话，另签一枚以租户 ID
// 为 buyerIdentity 的顾客令牌，拼进 URL 片段。buyerIdentity 会作为
// merchantProvidedBuyerIdentity 出现在该订单之后的每个 Webhook 里——结账页
// 「自愈」重建会话时 orderMetadata 与 orderMerchantExternalId 都会丢，它不会。
//
// 令牌只存在于这次响应里：不落库、不进日志。重复提交同一个幂等键时重新签发。
//
// requestBase 是发起请求的站点地址（scheme://host），用来推导付款后的回跳地址。
func (s *BillingService) CreateCheckout(ctx context.Context, tenantID, userID, priceID, idempotencyKey, requestBase string) (*model.BillingCheckout, error) {
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	price, err := s.checkoutPreconditions(ctx, tenantID, priceID, idempotencyKey)
	if err != nil {
		return nil, err
	}
	// 同一个键重复提交（网络重试）：会话还有效就复用，只换一枚新令牌。
	if old, err := s.store.GetCheckoutByIdempotency(ctx, tenantID, idempotencyKey); err == nil {
		if old.Status == "pending" && old.CheckoutURL != "" && old.ExpiresAt != nil && time.Until(*old.ExpiresAt) > time.Minute {
			return s.withBuyerToken(ctx, old, tenantID, price.ProviderProductID)
		}
		return nil, ErrCheckoutStale
	} else if !errors.Is(err, repo.ErrNotFound) {
		return nil, err
	}

	checkout := model.BillingCheckout{
		ID: uuid.NewString(), TenantID: tenantID, PlanPriceID: price.ID,
		MerchantExternalID: "EBX-" + uuid.NewString(), IdempotencyKey: idempotencyKey, Status: "pending",
	}
	if err := s.store.CreateCheckout(ctx, checkout); err != nil {
		return nil, err
	}
	req := waffo.CheckoutRequest{
		ProductID: price.ProviderProductID, Currency: price.Currency,
		SuccessURL: s.successURLFor(requestBase), ExpiresInSeconds: int(checkoutTTL / time.Second), Language: "zh-Hans",
		OrderMerchantExternalID: checkout.MerchantExternalID,
		Metadata: map[string]string{
			"tenant_id": tenantID, "plan_id": price.PlanID, "plan_price_id": price.ID, "checkout_id": checkout.ID,
		},
	}
	// 用户邮箱只用来预填结账页；注册时邮箱是可选的，没有就让买家自己填。
	if user, err := s.store.GetUserByID(ctx, userID); err == nil {
		req.BuyerEmail = user.Email
	}
	session, err := s.waffo.CreateCheckout(ctx, req, "ebx-checkout-"+checkout.ID)
	if err != nil {
		checkout.Status = "failed"
		return nil, errors.Join(err, s.store.UpdateCheckout(ctx, checkout))
	}
	expires := session.ExpiresAt
	if expires.IsZero() {
		expires = time.Now().Add(checkoutTTL)
	}
	checkout.CheckoutURL, checkout.ProviderSessionID, checkout.ExpiresAt = session.CheckoutURL, session.SessionID, &expires
	if err := s.store.UpdateCheckout(ctx, checkout); err != nil {
		return nil, err
	}
	return s.withBuyerToken(ctx, &checkout, tenantID, price.ProviderProductID)
}

// checkoutPreconditions 检查这次结账能不能发起：支付已打开、配置就绪、价格可售、
// 租户没有仍占着位置的订阅。返回要购买的价格。
func (s *BillingService) checkoutPreconditions(ctx context.Context, tenantID, priceID, idempotencyKey string) (*model.PlanPrice, error) {
	settings, err := s.store.GetBillingSettings(ctx)
	if err != nil {
		return nil, err
	}
	if !settings.Enabled {
		return nil, ErrBillingDisabled
	}
	if s.waffo == nil || s.config.StoreID == "" {
		return nil, ErrBillingNotReady
	}
	if idempotencyKey == "" || len(idempotencyKey) > 128 {
		return nil, errors.New("idempotency_key 必填，且不超过 128 个字符")
	}
	price, err := s.purchasablePrice(ctx, priceID)
	if err != nil {
		return nil, err
	}
	// 先对账再判断：Webhook 迟到时本地还不知道上一笔已经付过，直接放行就会让用户
	// 同时背两份订阅、两份扣款（测试中真的出现过）。没有待确认的结账时这一步不请求 Waffo。
	sub, _, err := s.SyncCheckouts(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	if sub != nil && blockingSubscriptionStatus[sub.Status] {
		return nil, ErrSubscriptionExists
	}
	return price, nil
}

// purchasablePrice 取出可以购买的价格：已上架、已同步到 Waffo、不属于默认套餐。
func (s *BillingService) purchasablePrice(ctx context.Context, priceID string) (*model.PlanPrice, error) {
	price, err := s.store.GetPlanPrice(ctx, priceID)
	if err != nil {
		return nil, err
	}
	if !price.Active || price.SyncStatus != "active" || price.ProviderProductID == "" {
		return nil, ErrBillingNotReady
	}
	// 可购列表已经排除了默认套餐；直接拿旧价格 ID 调接口的也要挡住。
	plan, err := s.store.GetPlanByID(ctx, price.PlanID)
	if err != nil {
		return nil, err
	}
	if plan.IsDefault {
		return nil, fmt.Errorf("%w：默认套餐不能购买", ErrInvalidPrice)
	}
	return price, nil
}

// withBuyerToken 签发顾客令牌并拼出买家打开的地址：{checkoutUrl}?test=true#token=...
//
// 令牌必须放在片段里：结账页首屏读取后会把它从地址栏抹掉，片段也不会进服务器日志和
// Referer；放在查询串里页面根本不读。?test=true 让测试环境的首屏与回退路径停在测试环境。
func (s *BillingService) withBuyerToken(ctx context.Context, checkout *model.BillingCheckout, tenantID, productID string) (*model.BillingCheckout, error) {
	token, err := s.waffo.IssueSessionToken(ctx, waffo.SessionTokenRequest{ProductID: productID, BuyerIdentity: tenantID})
	if err != nil {
		return nil, err
	}
	u, err := url.Parse(checkout.CheckoutURL)
	if err != nil {
		return nil, err
	}
	if s.config.Environment == "test" {
		q := u.Query()
		q.Set("test", "true")
		u.RawQuery = q.Encode()
	}
	u.Fragment = "token=" + token
	out := *checkout
	out.CheckoutURL = u.String()
	return &out, nil
}

// Cancel 请求周期末取消（cancel=true）或撤销取消（cancel=false）。
//
// 本地状态在 Waffo 接受请求后立即更新，并把 last_event_at 记成现在：
// 随后到达的、时间更早的 canceling / uncanceled 事件会被当成旧事件忽略，
// 不会把用户刚做的选择翻回去。最终状态仍以之后的 Webhook 为准。
func (s *BillingService) Cancel(ctx context.Context, tenantID string, cancel bool) error {
	sub, err := s.store.GetSubscription(ctx, tenantID)
	if err != nil {
		return err
	}
	if s.waffo == nil {
		return ErrBillingNotReady
	}
	status := "canceling"
	if cancel {
		if sub.Status != "active" {
			return errors.New("只有生效中的订阅可以设置到期取消")
		}
		// 幂等键绑定订单：重复点击或超时重试不会产生第二次取消请求。
		if err := s.waffo.CancelSubscription(ctx, sub.OrderID, "ebx-cancel-"+sub.OrderID); err != nil {
			return err
		}
	} else {
		if sub.Status != "canceling" {
			return errors.New("当前订阅没有设置到期取消")
		}
		token, err := s.waffo.IssueSessionToken(ctx, waffo.SessionTokenRequest{StoreID: s.config.StoreID, BuyerIdentity: tenantID})
		if err != nil {
			return err
		}
		if err := s.waffo.ReactivateSubscription(ctx, sub.OrderID, token, sub.Mode); err != nil {
			return err
		}
		status = "active"
	}
	now := time.Now().UTC()
	return s.store.UpdateSubscriptionStatus(ctx, tenantID, status, cancel, sub.CurrentPeriodStart, sub.CurrentPeriodEnd, &now)
}

// syncWindow 是对账回看的范围。结账会话 30 分钟过期，一天足够覆盖
// 「付完款过了很久才回到页面」，又不至于每次都去问一长串早已放弃的结账。
const syncWindow = 24 * time.Hour

// SyncCheckouts 向 Waffo 查询该租户最近未确认的结账，把已经生效的订阅入账，返回当前订阅。
// changed 表示这次对账改了订阅，前端据此重取配额。
//
// Webhook 仍是权益的主路径；这里补的是它到不了的情况：本地开发时 Waffo 推不到 localhost，
// 生产上也可能因为网络或配置丢掉回调。查到的是 Waffo 自己的订单状态，
// 调用方除了租户身份之外不提供任何输入，因此不会被用来伪造权益。
// 没有待确认的结账时只读数据库，不产生上游请求。
//
// changed 比较的是对账前后的订阅本身，而不是「有没有执行入账」：Webhook 先到的话，
// 对账查到的订单会被乱序保护跳过，那不算变化——算成变化会让前端每次都重取、再对账，转个不停。
func (s *BillingService) SyncCheckouts(ctx context.Context, tenantID string) (*model.Subscription, bool, error) {
	before, err := s.GetSubscription(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}
	if s.waffo == nil || s.config.StoreID == "" {
		return before, false, nil
	}
	pending, err := s.store.ListPendingCheckouts(ctx, tenantID, time.Now().Add(-syncWindow))
	if err != nil {
		return nil, false, err
	}
	if len(pending) == 0 {
		return before, false, nil
	}
	for _, checkout := range pending {
		if err := s.syncCheckout(ctx, tenantID, checkout); err != nil {
			// 对账是尽力而为：Waffo 一时不可用时照常返回库里的状态，Webhook 与下次对账还会补上。
			slog.Warn("对账 Waffo 结账失败", "checkout_id", checkout.ID, "error", err)
		}
	}
	after, err := s.GetSubscription(ctx, tenantID)
	if err != nil {
		return nil, false, err
	}
	return after, subscriptionKey(before) != subscriptionKey(after), nil
}

func subscriptionKey(sub *model.Subscription) string {
	if sub == nil {
		return ""
	}
	return sub.OrderID + "|" + sub.Status + "|" + sub.PlanID
}

// liveOrderStatus 是可以入账的订单状态：付款已成功、订阅在服务期内。
var liveOrderStatus = map[string]bool{"active": true, "canceling": true, "past_due": true}

func (s *BillingService) syncCheckout(ctx context.Context, tenantID string, checkout model.BillingCheckout) error {
	order, err := s.waffo.FindSubscriptionOrder(ctx, s.config.StoreID, checkout.MerchantExternalID)
	if err != nil {
		return err
	}
	if order == nil {
		// 会话过期且没有产生订单：买家放弃了。标成 expired，之后不再去问。
		if checkout.ExpiresAt != nil && time.Since(*checkout.ExpiresAt) > 5*time.Minute {
			checkout.Status = "expired"
			return s.store.UpdateCheckout(ctx, checkout)
		}
		return nil
	}
	if !liveOrderStatus[order.Status] {
		return nil
	}
	// 按一次「订阅激活」事件入账，与 Webhook 走同一套状态机与乱序保护。
	// 事件时间取订单创建时间：之后真正到达的 Webhook 都比它新，照常生效。
	ev := webhookEnvelope{EventType: "subscription.activated", Mode: s.config.Environment, Timestamp: order.CreatedAt}
	data := webhookData{
		OrderID: order.ID, OrderMerchantExternalID: checkout.MerchantExternalID, MerchantIdentity: tenantID,
		CurrentPeriodStart: order.CurrentPeriodStart, CurrentPeriodEnd: order.CurrentPeriodEnd,
	}
	eventAt := time.Now().UTC()
	if t := parseWaffoTime(order.CreatedAt); t != nil {
		eventAt = *t
	}
	return s.store.WithTx(ctx, func(tx *repo.Store) error {
		owner, priceID, err := resolveOwner(ctx, tx, data)
		if err != nil {
			return err
		}
		current, err := tx.GetSubscription(ctx, owner)
		if errors.Is(err, repo.ErrNotFound) {
			current = nil
		} else if err != nil {
			return err
		}
		if err := applyStatus(ctx, tx, ev, order.Status, owner, priceID, current, data, eventAt); err != nil {
			return err
		}
		// 订单已经生效，这次结账就算完成了——不论上面是入了账，还是 Webhook 早一步入过、
		// 这次被乱序保护跳过。不标完成的话它会一直留在待对账列表里，每次打开页面都去问一遍。
		return tx.MarkCheckoutCompleted(ctx, checkout.MerchantExternalID)
	})
}
