package service

import (
	"context"
	"crypto/rsa"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"emailbox/configs"
	"emailbox/pkg/model"
	"emailbox/pkg/repo"
	"emailbox/pkg/waffo"

	"github.com/google/uuid"
)

var (
	ErrBillingDisabled    = errors.New("支付入口已关闭")
	ErrBillingNotReady    = errors.New("支付配置尚未就绪")
	ErrSubscriptionExists = errors.New("已有生效中的订阅，请先在当前订阅到期后再购买")
	ErrInvalidPrice       = errors.New("套餐价格参数无效")
	ErrWebhookRejected    = errors.New("webhook 校验失败")
	// ErrCheckoutStale 表示同一个幂等键对应的结账已失败或过期。
	// 不在原记录上重来：那个键在 Waffo 侧已经缓存了旧会话（24 小时），换新键才安全。
	ErrCheckoutStale = errors.New("这次结账已失效，请重新发起")
	// ErrWebhookUnprocessable 是重试也救不回来的事件（找不到租户、价格、归属冲突）。
	// 这类事件记为 failed 并回 2xx：让 Waffo 重试三遍只会得到三次同样的失败。
	ErrWebhookUnprocessable = errors.New("webhook 事件无法处理")
)

// subscriptionCurrencies 是 Waffo 订阅产品支持的币种（见 create-session 的错误表）。
var subscriptionCurrencies = map[string]bool{"USD": true, "EUR": true, "GBP": true, "HKD": true, "JPY": true}

var amountPattern = regexp.MustCompile(`^\d+(\.\d{1,2})?$`)

// BillingService 负责支付开关、价格与 Waffo 产品同步、结账、取消与 Webhook 权益变更。
// 私钥与 Webhook 公钥在构造时解析一次，之后只以解析后的对象留在内存里。
type BillingService struct {
	store       *repo.Store
	waffo       *waffo.Client
	config      configs.WaffoConfig
	webhookKeys map[string]*rsa.PublicKey
	successURL  string
}

func NewBillingService(store *repo.Store, cfg configs.WaffoConfig) (*BillingService, error) {
	s := &BillingService{store: store, config: cfg, webhookKeys: map[string]*rsa.PublicKey{}}
	// WAFFO_SUCCESS_URL 是可选覆盖，留空时按请求域名推导（successURLFor）。
	// 显式写了就必须是 https：Waffo 创建会话时接受非 https 的值，却会在买家付款那一步
	// 拒掉（[A0003] successRedirectUrl is invalid），买家只看到一个笼统的「支付失败」。
	if cfg.SuccessURL != "" {
		if u, err := url.Parse(cfg.SuccessURL); err != nil || u.Scheme != "https" || u.Host == "" {
			return nil, errors.New("WAFFO_SUCCESS_URL 必须是 https 地址，或留空按访问域名自动推导")
		}
		s.successURL = cfg.SuccessURL
	}
	// 凭据没配时照样构造：支付不可用，服务照常启动（09 文档 §3.1）。
	// 配了却解析不了必须启动失败——放过去的话，管理员只会在后台看到一句「缺少配置」。
	if cfg.MerchantID != "" && strings.TrimSpace(cfg.PrivateKey) != "" {
		client, err := waffo.New(waffo.Config{BaseURL: cfg.APIBaseURL, MerchantID: cfg.MerchantID, PrivateKey: cfg.PrivateKey})
		if err != nil {
			return nil, fmt.Errorf("WAFFO_PRIVATE_KEY 无法使用: %w", err)
		}
		s.waffo = client
	}
	for mode, override := range map[string]string{"test": cfg.WebhookTestKey, "prod": cfg.WebhookProdKey} {
		raw := override
		if strings.TrimSpace(raw) == "" {
			raw = waffo.PlatformPublicKey(mode)
		}
		key, err := waffo.ParsePublicKey(raw)
		if err != nil {
			return nil, fmt.Errorf("WAFFO_WEBHOOK_%s_PUBLIC_KEY 无法解析: %w", strings.ToUpper(mode), err)
		}
		s.webhookKeys[mode] = key
	}
	return s, nil
}

// successURLFor 决定付款完成后「完成」按钮带买家回到哪里。
//
// 没有显式配置时用发起结账的那次请求的域名拼出用量页地址：部署在哪个域名就回哪个域名，
// 不必再多一个要和域名保持同步的变量。只有 https 才发（理由见构造函数）；
// 本地 http 开发时不发，买家停在 Waffo 自己的成功页。
// 域名取自请求头，伪造它只会影响伪造者自己这次结账的回跳，影响不到别人。
func (s *BillingService) successURLFor(requestBase string) string {
	if s.successURL != "" {
		return s.successURL
	}
	u, err := url.Parse(requestBase)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ""
	}
	return "https://" + u.Host + "/settings/usage?billing=success"
}

// missingConfig 列出打开支付之前还缺什么。给管理员看的是「去配哪个环境变量」，
// 而不是一句「配置未就绪」让人自己猜。
//
// 返回值永远不是 nil：nil 切片会被序列化成 null，前端读 missing.length 时整页崩掉。
func (s *BillingService) missingConfig(ctx context.Context) ([]string, error) {
	missing := []string{}
	if s.waffo == nil || s.config.StoreID == "" {
		missing = append(missing, "服务器环境变量 WAFFO_MERCHANT_ID、WAFFO_STORE_ID、WAFFO_PRIVATE_KEY（改完需重启服务）")
	}
	prices, err := s.store.ListPlanPrices(ctx, true)
	if err != nil {
		return nil, err
	}
	if len(prices) == 0 {
		missing = append(missing, "在下方为至少一个套餐设置价格并同步成功")
	}
	return missing, nil
}

func (s *BillingService) Settings(ctx context.Context) (*model.BillingSettings, error) {
	v, err := s.store.GetBillingSettings(ctx)
	if err != nil {
		return nil, err
	}
	missing, err := s.missingConfig(ctx)
	if err != nil {
		return nil, err
	}
	v.Environment = s.config.Environment
	v.Missing = missing
	return v, nil
}

// UpdateSettings 打开 / 关闭支付入口。环境不由管理员选：API Key 在创建时就绑定了
// test 或 prod，库里的 mode 只能跟着部署配置走，否则会出现「界面说生产、实际打测试」。
func (s *BillingService) UpdateSettings(ctx context.Context, v model.BillingSettings, actor string) (*model.BillingSettings, error) {
	if !subscriptionCurrencies[v.DefaultCurrency] {
		return nil, fmt.Errorf("%w：币种只支持 USD、EUR、GBP、HKD、JPY", ErrInvalidPrice)
	}
	if v.Enabled {
		missing, err := s.missingConfig(ctx)
		if err != nil {
			return nil, err
		}
		if len(missing) > 0 {
			return nil, fmt.Errorf("%w：缺少 %s", ErrBillingNotReady, strings.Join(missing, "、"))
		}
	}
	v.Mode = s.config.Environment
	v.UpdatedBy = actor
	if err := s.store.UpdateBillingSettings(ctx, v); err != nil {
		return nil, err
	}
	return s.Settings(ctx)
}

func (s *BillingService) ListPrices(ctx context.Context, activeOnly bool) ([]model.PlanPrice, error) {
	return s.store.ListPlanPrices(ctx, activeOnly)
}

func validatePrice(p model.PlanPrice) error {
	if p.BillingPeriod != "monthly" && p.BillingPeriod != "yearly" {
		return fmt.Errorf("%w：计费周期只能是月付或年付", ErrInvalidPrice)
	}
	if !subscriptionCurrencies[p.Currency] {
		return fmt.Errorf("%w：币种只支持 USD、EUR、GBP、HKD、JPY", ErrInvalidPrice)
	}
	if !amountPattern.MatchString(p.Amount) {
		return fmt.Errorf("%w：金额写成 9 或 9.90 这样的格式，最多两位小数", ErrInvalidPrice)
	}
	// 只用来判断是否为 0，金额本身始终以字符串保存与传递。
	if f, err := strconv.ParseFloat(p.Amount, 64); err != nil || f <= 0 {
		return fmt.Errorf("%w：金额必须大于 0", ErrInvalidPrice)
	}
	return nil
}

// CreatePrice 保存价格变体，并尝试同步成 Waffo 产品。
// 同步失败不回滚：本地草稿留着，管理员看到同步错误后可以重试（09 文档 §5.1）。
func (s *BillingService) CreatePrice(ctx context.Context, p model.PlanPrice) (*model.PlanPrice, error) {
	if err := validatePrice(p); err != nil {
		return nil, err
	}
	plan, err := s.store.GetPlanByID(ctx, p.PlanID)
	if err != nil {
		return nil, err
	}
	// 默认套餐是每个人注册即有的，订阅取消后也回落到它；给它标价等于卖一份免费的东西。
	if plan.IsDefault {
		return nil, fmt.Errorf("%w：默认套餐不能设置价格", ErrInvalidPrice)
	}
	p.ID = uuid.NewString()
	p.SyncStatus, p.SyncError, p.ProviderProductID, p.Active = "pending", "", "", true
	if err := s.store.CreatePlanPrice(ctx, p); err != nil {
		return nil, err
	}
	if s.waffo == nil || s.config.StoreID == "" {
		return s.store.GetPlanPrice(ctx, p.ID)
	}
	return s.SyncPrice(ctx, p.ID)
}

// UpdatePrice 改金额或上下架。币种不能改：已同步的产品按币种挂价格，
// 改币种等于换一个商品，应当新建价格，而不是让旧订阅的续费价格悄悄变了含义。
//
// active 为 nil 表示不改上下架状态：PATCH 只改金额时不能顺手把价格下架了。
func (s *BillingService) UpdatePrice(ctx context.Context, id, amount, currency string, active *bool) (*model.PlanPrice, error) {
	old, err := s.store.GetPlanPrice(ctx, id)
	if err != nil {
		return nil, err
	}
	p := model.PlanPrice{ID: id, Amount: amount, Currency: currency, Active: old.Active}
	if p.Amount == "" {
		p.Amount = old.Amount
	}
	if active != nil {
		p.Active = *active
	}
	p.PlanID, p.BillingPeriod = old.PlanID, old.BillingPeriod
	if p.Currency == "" {
		p.Currency = old.Currency
	}
	if p.Currency != old.Currency && old.ProviderProductID != "" {
		return nil, fmt.Errorf("%w：已同步到 Waffo 的价格不能改币种，请新建一个价格", ErrInvalidPrice)
	}
	if err := validatePrice(p); err != nil {
		return nil, err
	}
	next := *old
	next.Amount, next.Currency, next.Active = p.Amount, p.Currency, p.Active
	changed := next.Amount != old.Amount || next.Currency != old.Currency
	if changed {
		next.SyncStatus, next.SyncError = "pending", ""
	}
	if err := s.store.UpdatePlanPrice(ctx, next); err != nil {
		return nil, err
	}
	if changed && s.waffo != nil && s.config.StoreID != "" {
		return s.SyncPrice(ctx, p.ID)
	}
	return s.store.GetPlanPrice(ctx, p.ID)
}

// SyncPrice 把价格推到 Waffo：没有产品 ID 就建，有就更新（Waffo 会生成新版本，
// 已有订阅继续用旧版本续费）。结果写回 sync_status / sync_error。
func (s *BillingService) SyncPrice(ctx context.Context, id string) (*model.PlanPrice, error) {
	price, err := s.store.GetPlanPrice(ctx, id)
	if err != nil {
		return nil, err
	}
	if s.waffo == nil || s.config.StoreID == "" {
		return nil, ErrBillingNotReady
	}
	name := price.PlanName + map[string]string{"monthly": "（月付）", "yearly": "（年付）"}[price.BillingPeriod]
	prices := map[string]waffo.PriceInfo{price.Currency: {Amount: price.Amount, TaxCategory: "saas"}}
	metadata := map[string]string{"plan_id": price.PlanID, "plan_price_id": price.ID}
	if price.ProviderProductID == "" {
		// 幂等键绑定价格 ID：同步请求超时后重试，Waffo 在 24 小时内返回同一个产品，
		// 而不是再建一个同名产品。
		var productID string
		productID, err = s.waffo.CreateSubscriptionProduct(ctx, waffo.SubscriptionProductRequest{
			StoreID: s.config.StoreID, Name: name, BillingPeriod: price.BillingPeriod,
			Prices: prices, Metadata: metadata,
		}, "ebx-product-"+price.ID)
		if err == nil {
			price.ProviderProductID = productID
		}
	} else {
		err = s.waffo.UpdateSubscriptionProduct(ctx, waffo.UpdateSubscriptionProductRequest{
			ID: price.ProviderProductID, Name: name, BillingPeriod: price.BillingPeriod,
			Prices: prices, Metadata: metadata,
		}, "")
	}
	if err != nil {
		price.SyncStatus, price.SyncError = "error", syncErrorMessage(err)
		slog.Warn("Waffo 产品同步失败", "plan_price_id", price.ID, "error", err)
	} else {
		price.SyncStatus, price.SyncError = "active", ""
	}
	if err := s.store.UpdatePlanPrice(ctx, *price); err != nil {
		return nil, err
	}
	return s.store.GetPlanPrice(ctx, id)
}

// syncErrorMessage 给管理员看的同步错误：Waffo 的错误文案（不含凭据）或网络错误类别。
func syncErrorMessage(err error) string {
	var apiErr *waffo.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Error()
	}
	return "无法连接 Waffo，请稍后重试"
}
