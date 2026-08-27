package model

import "time"

// AdminUser 是管理后台用户列表里的一行：用户本体 + 他的租户空间 + 邮箱数 + 配额。
// 全部在一次查询里取齐，列表页因此不会退化成 N+1。
//
// **一个租户空间只属于一个用户**，所以后台不再单列一份「工作空间」清单——
// 那份清单的每一行都能在这里找到对应的用户，两份列表只会让人怀疑
// 「这两个数为什么对不上」。配额与「进入其邮箱」因此都并到了这一行上。
type AdminUser struct {
	ID           string       `json:"id"`
	Username     string       `json:"username"`
	Email        string       `json:"email"`
	Status       UserStatus   `json:"status"`
	PlatformRole PlatformRole `json:"platform_role"`
	CreatedAt    time.Time    `json:"created_at"`
	LastLoginAt  *time.Time   `json:"last_login_at"`
	// TenantID 为空表示这个用户没有租户空间。正常注册流程不会产生这种用户
	// （注册是事务化的五件套），但 000002_saas 之前建的老账号会。
	TenantID     string `json:"tenant_id"`
	TenantName   string `json:"tenant_name"`
	AccountCount int    `json:"account_count"`
	PlanCode     string `json:"plan_code"`
	MaxAccounts  int    `json:"max_accounts"`
	// OverQuota 标记「现有账号数已经超过上限」。调低配额不追溯删除已有数据
	// （08 文档 §4.2），因此这个状态是合法且会长期存在的，后台要能一眼看见。
	OverQuota bool `json:"over_quota"`
}

// ComputeOverQuota 由账号数与上限推出超额标记。Unlimited 永不超额。
func (u *AdminUser) ComputeOverQuota() {
	u.OverQuota = u.MaxAccounts != Unlimited && u.AccountCount > u.MaxAccounts
}

// AdminUserFilter 是用户列表的查询条件，空值表示该项不筛选。
type AdminUserFilter struct {
	Query        string
	Status       string
	PlatformRole string
	Page         int
	Limit        int
}

func (f *AdminUserFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
}

func (f AdminUserFilter) Offset() int { return (f.Page - 1) * f.Limit }

// PlatformStats 是 /admin 总览卡片的数据。
type PlatformStats struct {
	UserCount          int `json:"user_count"`
	DisabledUserCount  int `json:"disabled_user_count"`
	AdminCount         int `json:"admin_count"`
	TenantCount        int `json:"tenant_count"`
	AccountCount       int `json:"account_count"`
	BannedAccountCount int `json:"banned_account_count"`
	MailFetchToday     int `json:"mail_fetch_today"`
	TokenRefreshToday  int `json:"token_refresh_today"`
}
