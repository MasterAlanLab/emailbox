package model

import "time"

// Unlimited 是配额列里表示「不限」的哨兵值。
const Unlimited = -1

// 按天累加的用量指标名，对应 usage_counters.metric。
//
// MetricMailFetch 记全部来源的取件（网页 / API Key / 管理员），共用一条上限——
// 只限 API 等于留了个「逆向网页就能绕开」的口子。
// MetricTokenRefresh 只记账不设限，见 quota.Service.Record。
const (
	MetricMailFetch    = "mail_fetch"
	MetricTokenRefresh = "token_refresh"
)

// Plan 是套餐，提供配额的基线值。
type Plan struct {
	ID             string    `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	IsDefault      bool      `json:"is_default"`
	MaxAccounts    int       `json:"max_accounts"`
	MaxGroups      int       `json:"max_groups"`
	DailyMailFetch int       `json:"daily_mail_fetch"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Limits 是某个租户的生效配额：套餐基线值经租户覆盖值 COALESCE 之后的结果。
type Limits struct {
	PlanCode       string `json:"plan_code"`
	PlanName       string `json:"plan_name"`
	MaxAccounts    int    `json:"max_accounts"`
	MaxGroups      int    `json:"max_groups"`
	DailyMailFetch int    `json:"daily_mail_fetch"`
}

// LimitFor 返回某个按天计数指标的上限，未知指标视为不限。
// token_refresh 就落在「不限」这一支：它只记账，见 quota.Service.Record。
func (l Limits) LimitFor(metric string) int {
	if metric == MetricMailFetch {
		return l.DailyMailFetch
	}
	return Unlimited
}
