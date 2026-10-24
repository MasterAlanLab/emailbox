package model

import "time"

// BillingSettings 是支付开关。Environment 与 Missing 不入库：前者来自部署配置
// （API Key 绑定的环境），后者列出打开支付之前还缺的配置，管理员据此知道该去配什么。
type BillingSettings struct {
	Enabled         bool      `json:"enabled"`
	Mode            string    `json:"mode"`
	DefaultCurrency string    `json:"default_currency"`
	UpdatedBy       string    `json:"updated_by,omitempty"`
	UpdatedAt       time.Time `json:"updated_at"`
	Environment     string    `json:"environment"`
	Missing         []string  `json:"missing"`
}

// PublicPricing 只包含访客了解费用所需的信息，避免公开支付配置与供应商标识。
type PublicPricing struct {
	BillingEnabled bool             `json:"billing_enabled"`
	FreePlan       PublicPlan       `json:"free_plan"`
	PaidPlans      []PublicPaidPlan `json:"paid_plans"`
}

type PublicPlan struct {
	Name           string `json:"name"`
	MaxAccounts    int    `json:"max_accounts"`
	MaxGroups      int    `json:"max_groups"`
	DailyMailFetch int    `json:"daily_mail_fetch"`
}

type PublicPaidPlan struct {
	PublicPlan
	Prices []PublicPrice `json:"prices"`
}

type PublicPrice struct {
	BillingPeriod string `json:"billing_period"`
	Currency      string `json:"currency"`
	Amount        string `json:"amount"`
}

type PlanPrice struct {
	ID                string    `json:"id"`
	PlanID            string    `json:"plan_id"`
	PlanCode          string    `json:"plan_code"`
	PlanName          string    `json:"plan_name"`
	BillingPeriod     string    `json:"billing_period"`
	Currency          string    `json:"currency"`
	Amount            string    `json:"amount"`
	ProviderProductID string    `json:"provider_product_id,omitempty"`
	SyncStatus        string    `json:"sync_status"`
	SyncError         string    `json:"sync_error,omitempty"`
	Active            bool      `json:"active"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

type Subscription struct {
	ID                 string     `json:"id"`
	TenantID           string     `json:"tenant_id"`
	Provider           string     `json:"provider"`
	Mode               string     `json:"mode"`
	OrderID            string     `json:"order_id"`
	ProviderProductID  string     `json:"provider_product_id"`
	PlanPriceID        string     `json:"plan_price_id"`
	PlanID             string     `json:"plan_id"`
	Status             string     `json:"status"`
	Currency           string     `json:"currency"`
	Amount             string     `json:"amount"`
	CurrentPeriodStart *time.Time `json:"current_period_start,omitempty"`
	CurrentPeriodEnd   *time.Time `json:"current_period_end,omitempty"`
	CancelAtPeriodEnd  bool       `json:"cancel_at_period_end"`
	LastEventAt        *time.Time `json:"last_event_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

type BillingCheckout struct {
	ID                 string     `json:"id"`
	TenantID           string     `json:"tenant_id"`
	PlanPriceID        string     `json:"plan_price_id"`
	MerchantExternalID string     `json:"merchant_external_id"`
	ProviderSessionID  string     `json:"provider_session_id"`
	IdempotencyKey     string     `json:"idempotency_key"`
	CheckoutURL        string     `json:"checkout_url"`
	Status             string     `json:"status"`
	ExpiresAt          *time.Time `json:"expires_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
}
