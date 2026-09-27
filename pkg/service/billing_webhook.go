package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/waffo"

	"github.com/google/uuid"
)

// 订阅状态事件到本地状态的映射。未列出的事件只登记、不改权益：
//   - subscription.payment_succeeded 只描述一次扣款，周期滚动由 renewed 负责
//   - plan_change_scheduled / plan_change_failed 不改变本周期的套餐
//   - refund.*：退款不自动回收权益。回收只跟着 subscription.canceled 走，
//     需要立即停权的退款由管理员在 Waffo 侧取消订阅来触发——一笔部分退款
//     就把用户降级，是比「多用几天」严重得多的错误
var subscriptionStatusByEvent = map[string]string{
	"subscription.activated":    "active",
	"subscription.renewed":      "active",
	"subscription.recovered":    "active",
	"subscription.uncanceled":   "active",
	"subscription.plan_changed": "active",
	"subscription.canceling":    "canceling",
	"subscription.past_due":     "past_due",
}

type webhookEnvelope struct {
	ID        string          `json:"id"`
	EventID   string          `json:"eventId"`
	EventType string          `json:"eventType"`
	StoreID   string          `json:"storeId"`
	Mode      string          `json:"mode"`
	Timestamp string          `json:"timestamp"`
	Data      json.RawMessage `json:"data"`
}

// webhookData 只取用得上的字段。日期全部按字符串读：currentPeriodStart / End
// 是「2026-03-10」这样的纯日期，直接解进 time.Time 会让整个事件解析失败。
type webhookData struct {
	OrderID                 string            `json:"orderId"`
	Currency                string            `json:"currency"`
	Amount                  string            `json:"amount"`
	MerchantIdentity        string            `json:"merchantProvidedBuyerIdentity"`
	OrderMetadata           map[string]string `json:"orderMetadata"`
	ProductMetadata         map[string]string `json:"productMetadata"`
	OrderMerchantExternalID string            `json:"orderMerchantExternalId"`
	CurrentPeriodStart      string            `json:"currentPeriodStart"`
	CurrentPeriodEnd        string            `json:"currentPeriodEnd"`
	PlanPrice               struct {
		Total string `json:"total"`
	} `json:"planPrice"`
}

func unprocessable(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrWebhookUnprocessable, fmt.Sprintf(format, args...))
}

// parseWaffoTime 认 ISO 8601 时间戳与纯日期两种写法；空串或认不出返回 nil。
func parseWaffoTime(v string) *time.Time {
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// VerifyWebhook 验签，并确认事件属于本部署的 Store 与环境。
// 环境不匹配直接拒绝：测试环境的事件绝不能改生产租户的套餐（09 文档 §1.2 第 5 条）。
func (s *BillingService) VerifyWebhook(signature string, body []byte, now time.Time) error {
	var ev webhookEnvelope
	if err := json.Unmarshal(body, &ev); err != nil {
		return ErrWebhookRejected
	}
	if s.config.StoreID == "" || ev.StoreID != s.config.StoreID || ev.Mode != s.config.Environment {
		return ErrWebhookRejected
	}
	if err := waffo.VerifyWebhook(signature, s.webhookKeys[ev.Mode], body, now); err != nil {
		return errors.Join(ErrWebhookRejected, err)
	}
	return nil
}

// ProcessWebhook 去重后在一个事务里应用事件。
//
// 去重键是（eventType, eventId, mode）：Waffo 的 eventId 按事件类型指向不同实体，
// 同一笔付款的 subscription.activated 与 subscription.payment_succeeded 共用一个 eventId，
// 只按 eventId 去重会把后到的那个当成重复静默丢掉。
//
// 已处理过的直接返回；之前失败或处理到一半（进程被杀）的重新处理——事件处理本身是
// 幂等的，靠的是订单号与事件时间的比较，而不是「只处理一次」。
func (s *BillingService) ProcessWebhook(ctx context.Context, body []byte) error {
	var ev webhookEnvelope
	if err := json.Unmarshal(body, &ev); err != nil {
		return unprocessable("事件不是合法 JSON")
	}
	if ev.EventID == "" {
		ev.EventID = ev.ID
	}
	if ev.EventID == "" || ev.EventType == "" {
		return unprocessable("事件缺少 eventId 或 eventType")
	}
	hash := sha256.Sum256(body)
	inserted, err := s.store.InsertWebhookEvent(ctx, ev.EventID, ev.Mode, ev.EventType, ev.StoreID, hex.EncodeToString(hash[:]))
	if err != nil {
		return err
	}
	if !inserted {
		status, err := s.store.GetWebhookEventStatus(ctx, ev.EventType, ev.EventID, ev.Mode)
		if err != nil || status == "processed" {
			return err
		}
	}
	procErr := s.store.WithTx(ctx, func(tx *repo.Store) error { return s.applyWebhook(ctx, tx, ev) })
	if procErr != nil {
		slog.Error("处理 Waffo Webhook 失败", "event_type", ev.EventType, "event_id", ev.EventID, "error", procErr)
		markErr := s.store.UpdateWebhookEvent(ctx, ev.EventType, ev.EventID, ev.Mode, "failed", truncateError(procErr.Error()))
		return errors.Join(procErr, markErr)
	}
	return s.store.UpdateWebhookEvent(ctx, ev.EventType, ev.EventID, ev.Mode, "processed", "")
}

func (s *BillingService) applyWebhook(ctx context.Context, tx *repo.Store, ev webhookEnvelope) error {
	status, isStatusEvent := subscriptionStatusByEvent[ev.EventType]
	if !isStatusEvent && ev.EventType != "subscription.canceled" {
		return nil
	}
	var data webhookData
	if err := json.Unmarshal(ev.Data, &data); err != nil {
		return unprocessable("data 字段无法解析")
	}
	if data.OrderID == "" {
		return unprocessable("事件缺少 orderId")
	}
	eventAt := time.Now().UTC()
	if t := parseWaffoTime(ev.Timestamp); t != nil {
		eventAt = *t
	}
	tenantID, priceID, err := resolveOwner(ctx, tx, data)
	if err != nil {
		return err
	}
	current, err := tx.GetSubscription(ctx, tenantID)
	if errors.Is(err, repo.ErrNotFound) {
		current = nil
	} else if err != nil {
		return err
	}
	if ev.EventType == "subscription.canceled" {
		return applyCanceled(ctx, tx, tenantID, current, data, eventAt)
	}
	return applyStatus(ctx, tx, ev, status, tenantID, priceID, current, data, eventAt)
}

// resolveOwner 找出事件属于哪个租户、哪个价格。
//
// 租户的几路来源都是我们自己在服务端写进去的（结账记录、orderMetadata、buyerIdentity、
// 已有订阅），出现分歧只可能是数据被篡改或串了单——直接拒绝，绝不猜。
// 价格按可信度取第一个：结账记录 > orderMetadata > productMetadata > 已有订阅。
func resolveOwner(ctx context.Context, tx *repo.Store, data webhookData) (tenantID, priceID string, err error) {
	var tenants, prices []string
	if data.OrderMerchantExternalID != "" {
		if checkout, err := tx.GetCheckoutByMerchantExternalID(ctx, data.OrderMerchantExternalID); err == nil {
			tenants, prices = append(tenants, checkout.TenantID), append(prices, checkout.PlanPriceID)
		} else if !errors.Is(err, repo.ErrNotFound) {
			return "", "", err
		}
	}
	tenants = append(tenants, data.OrderMetadata["tenant_id"], data.MerchantIdentity)
	prices = append(prices, data.OrderMetadata["plan_price_id"], data.ProductMetadata["plan_price_id"])
	if existing, err := tx.GetSubscriptionByOrderID(ctx, data.OrderID); err == nil {
		tenants, prices = append(tenants, existing.TenantID), append(prices, existing.PlanPriceID)
	} else if !errors.Is(err, repo.ErrNotFound) {
		return "", "", err
	}
	for _, t := range tenants {
		if t == "" {
			continue
		}
		if tenantID != "" && t != tenantID {
			return "", "", unprocessable("订单 %s 的租户归属不一致", data.OrderID)
		}
		tenantID = t
	}
	if tenantID == "" {
		return "", "", unprocessable("订单 %s 找不到所属租户", data.OrderID)
	}
	if _, err := tx.GetTenantByID(ctx, tenantID); errors.Is(err, repo.ErrNotFound) {
		return "", "", unprocessable("订单 %s 的租户已不存在", data.OrderID)
	} else if err != nil {
		return "", "", err
	}
	for _, p := range prices {
		if p != "" {
			return tenantID, p, nil
		}
	}
	return tenantID, "", nil
}

// applyStatus 应用 active / canceling / past_due 这类状态事件。
//
// 乱序保护有两条：
//   - 同一订单：事件时间早于已记录的最后事件时间，视为旧事件，丢弃
//   - 不同订单：只有 activated / plan_changed 能把租户切到新订单，且不能早于当前记录。
//     Waffo 的换套餐是「旧单取消 + 新单创建」，旧单迟到的 renewed 不能把新单顶掉
func applyStatus(
	ctx context.Context, tx *repo.Store, ev webhookEnvelope, status, tenantID, priceID string,
	current *model.Subscription, data webhookData, eventAt time.Time,
) error {
	starts := ev.EventType == "subscription.activated" || ev.EventType == "subscription.plan_changed"
	if current != nil {
		stale := current.LastEventAt != nil && eventAt.Before(*current.LastEventAt)
		if stale || (current.OrderID != data.OrderID && !starts) {
			return nil
		}
	}
	if priceID == "" {
		return unprocessable("订单 %s 找不到对应的价格", data.OrderID)
	}
	price, err := tx.GetPlanPrice(ctx, priceID)
	if errors.Is(err, repo.ErrNotFound) {
		return unprocessable("订单 %s 的价格已不存在", data.OrderID)
	} else if err != nil {
		return err
	}

	sub := model.Subscription{
		ID: uuid.NewString(), TenantID: tenantID, Provider: "waffo", Mode: ev.Mode, OrderID: data.OrderID,
		ProviderProductID: price.ProviderProductID, PlanPriceID: price.ID, PlanID: price.PlanID,
		Status: status, Currency: firstBillingValue(data.Currency, price.Currency),
		Amount:            firstBillingValue(data.PlanPrice.Total, data.Amount, price.Amount),
		CancelAtPeriodEnd: status == "canceling", LastEventAt: &eventAt,
		CurrentPeriodStart: parseWaffoTime(data.CurrentPeriodStart),
		CurrentPeriodEnd:   parseWaffoTime(data.CurrentPeriodEnd),
	}
	sameOrder := current != nil && current.OrderID == data.OrderID
	if current != nil {
		// 行按租户唯一，ID 保持不变：tenant_quotas.subscription_id 指着它。
		sub.ID = current.ID
	}
	if sameOrder {
		if sub.CurrentPeriodStart == nil {
			sub.CurrentPeriodStart = current.CurrentPeriodStart
		}
		if sub.CurrentPeriodEnd == nil {
			sub.CurrentPeriodEnd = current.CurrentPeriodEnd
		}
	}
	if err := tx.UpsertSubscription(ctx, sub); err != nil {
		return err
	}
	// 套餐只在订阅开始或换到新订单时授予。续费、恢复不重写 tenant_quotas：
	// 管理员若在订阅期间手工换了套餐，那是他的决定，续费不该把它覆盖回去。
	if starts || !sameOrder {
		if err := tx.UpdateTenantSubscriptionQuota(ctx, tenantID, price.PlanID, sub.ID); err != nil {
			return err
		}
	}
	if ev.EventType == "subscription.activated" && data.OrderMerchantExternalID != "" {
		return tx.MarkCheckoutCompleted(ctx, data.OrderMerchantExternalID)
	}
	return nil
}

// applyCanceled 处理订阅终止：标记 canceled 并回收由这份订阅授予的套餐。
// 只认当前订单——换套餐时旧单的 canceled 可能晚于新单的 activated 到达，
// 按租户直接回收会把刚付完钱的用户降回免费套餐。
func applyCanceled(
	ctx context.Context, tx *repo.Store, tenantID string, current *model.Subscription,
	data webhookData, eventAt time.Time,
) error {
	if current == nil || current.OrderID != data.OrderID || current.Status == "canceled" {
		return nil
	}
	if err := tx.UpdateSubscriptionStatus(ctx, tenantID, "canceled", false,
		current.CurrentPeriodStart, current.CurrentPeriodEnd, &eventAt); err != nil {
		return err
	}
	// 只回收 plan_source=subscription 且指向这份订阅的套餐；管理员手工设的保持原样。
	return tx.RestoreTenantQuotaSource(ctx, tenantID, current.ID)
}

func firstBillingValue(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
