package model

import "time"

// MaxBatchAccountIDs 是单次批量操作可传入的账号数上限。
// 超大参数会拖垮查询计划，且响应体里的逐项错误也会失控。
const MaxBatchAccountIDs = 5000

// MaxExportAccounts 是单次导出的账号数上限。导出结果整份留在内存里再写进响应，
// 没有上限的话一个租户的「导出全部」就能把进程撑爆。
const MaxExportAccounts = 20000

// 导出范围。
const (
	ExportScopeAll      = "all"
	ExportScopeGroup    = "group"
	ExportScopeSelected = "selected"
)

// ExportAccountsRequest 是导出请求（05 文档 §4.4）。
//
// PasswordConfirm 是操作者自己的登录密码：导出等价于取走全部凭据明文，
// 只认会话不够——会话可能是被盗的，也可能只是没锁屏的电脑被人借用了。
type ExportAccountsRequest struct {
	Scope           string   `json:"scope"`
	GroupIDs        []string `json:"group_ids"`
	AccountIDs      []string `json:"account_ids"`
	PasswordConfirm string   `json:"password_confirm"`
}

// AccountStatus 是账号的可用状态。banned 由协议层在识别到「账号被封」时置位。
type AccountStatus string

const (
	AccountStatusActive   AccountStatus = "active"
	AccountStatusDisabled AccountStatus = "disabled"
	AccountStatusBanned   AccountStatus = "banned"
)

func ValidAccountStatus(s AccountStatus) bool {
	switch s {
	case AccountStatusActive, AccountStatusDisabled, AccountStatusBanned:
		return true
	}
	return false
}

// RefreshStatus 是最近一次令牌刷新的结果。
type RefreshStatus string

const (
	RefreshNever   RefreshStatus = "never"
	RefreshSuccess RefreshStatus = "success"
	RefreshFailed  RefreshStatus = "failed"
)

// MailAccount 是一个托管的邮箱账号。
//
// 三个凭据字段是加密存储的密文，绝不能出现在任何列表/详情接口里——
// 只有导出接口（需 account:secret 权限 + 二次密码验证 + 审计）才返回明文。
type MailAccount struct {
	ID              string        `json:"id"`
	TenantID        string        `json:"-"`
	GroupID         string        `json:"group_id"`
	Email           string        `json:"email"`
	EmailNormalized string        `json:"-"`
	Provider        string        `json:"provider"`
	AccountType     string        `json:"account_type"`
	AuthChannel     string        `json:"auth_channel"`
	ClientID        string        `json:"client_id"`
	IMAPHost        string        `json:"imap_host"`
	IMAPPort        int           `json:"imap_port"`
	Status          AccountStatus `json:"status"`
	Remark          string        `json:"remark"`
	SortOrder       int           `json:"sort_order"`

	PasswordEnc     string `json:"-"`
	RefreshTokenEnc string `json:"-"`
	IMAPPasswordEnc string `json:"-"`

	ProxyURL          string `json:"-"`
	FallbackProxyURL1 string `json:"-"`
	FallbackProxyURL2 string `json:"-"`

	LastRefreshAt         *time.Time    `json:"last_refresh_at"`
	LastRefreshStatus     RefreshStatus `json:"last_refresh_status"`
	LastRefreshError      string        `json:"last_refresh_error"`
	RefreshTokenUpdatedAt *time.Time    `json:"refresh_token_updated_at"`

	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-"`
}

// MailAccountResponse 是账号的对外表示：凭据只回「有没有」，代理只回打码后的串。
type MailAccountResponse struct {
	MailAccount
	HasPassword     bool `json:"has_password"`
	HasRefreshToken bool `json:"has_refresh_token"`
	HasIMAPPassword bool `json:"has_imap_password"`

	ProxyURLMasked          string `json:"proxy_url_masked"`
	FallbackProxyURL1Masked string `json:"fallback_proxy_url_1_masked"`
	FallbackProxyURL2Masked string `json:"fallback_proxy_url_2_masked"`

	Aliases []string `json:"aliases"`
}

// AccountFilter 是账号列表的查询条件。零值表示不筛选。
//
// 这里**只放真正会被 SQL 用到的字段**：留着一个 repo 不读的字段，等于承诺了一种
// 并不存在的筛选，下一个人照着传参会得到「筛选没生效」且无从排查。
// 新增筛选维度时，连同两个方言的 SQL 与 parity 用例一起加。
type AccountFilter struct {
	GroupIDs      []string
	Query         string
	Status        string
	RefreshStatus string
	Provider      string
	Sort          string
	Order         string
	Page          int
	Limit         int
}

// 账号列表分页参数的边界。
const (
	DefaultAccountPageSize = 50
	MaxAccountPageSize     = 200
)

// Normalize 把分页与排序参数收敛到合法范围。
func (f *AccountFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit <= 0 {
		f.Limit = DefaultAccountPageSize
	}
	if f.Limit > MaxAccountPageSize {
		f.Limit = MaxAccountPageSize
	}
	switch f.Sort {
	case "created_at", "email", "sort_order", "last_refresh_at":
	default:
		f.Sort = "sort_order"
	}
	if f.Order != "asc" {
		f.Order = "desc"
	}
}

// Offset 返回 SQL 的 OFFSET。
func (f AccountFilter) Offset() int { return (f.Page - 1) * f.Limit }

type CreateMailAccountRequest struct {
	GroupID           string        `json:"group_id"`
	Email             string        `json:"email"`
	Provider          string        `json:"provider"`
	AccountType       string        `json:"account_type"`
	Password          string        `json:"password"`
	ClientID          string        `json:"client_id"`
	RefreshToken      string        `json:"refresh_token"`
	IMAPHost          string        `json:"imap_host"`
	IMAPPort          int           `json:"imap_port"`
	IMAPPassword      string        `json:"imap_password"`
	Status            AccountStatus `json:"status"`
	Remark            string        `json:"remark"`
	Aliases           []string      `json:"aliases"`
	ProxyURL          string        `json:"proxy_url"`
	FallbackProxyURL1 string        `json:"fallback_proxy_url_1"`
	FallbackProxyURL2 string        `json:"fallback_proxy_url_2"`
}

// UpdateMailAccountRequest 的字段用指针以区分「未提供」与「显式清空」。
// 凭据字段为 nil 表示保持原值——前端不会回显密文，所以不能把「没传」当成「清空」。
type UpdateMailAccountRequest struct {
	GroupID           *string        `json:"group_id"`
	Provider          *string        `json:"provider"`
	Password          *string        `json:"password"`
	ClientID          *string        `json:"client_id"`
	RefreshToken      *string        `json:"refresh_token"`
	IMAPHost          *string        `json:"imap_host"`
	IMAPPort          *int           `json:"imap_port"`
	IMAPPassword      *string        `json:"imap_password"`
	Status            *AccountStatus `json:"status"`
	Remark            *string        `json:"remark"`
	Aliases           *[]string      `json:"aliases"`
	ProxyURL          *string        `json:"proxy_url"`
	FallbackProxyURL1 *string        `json:"fallback_proxy_url_1"`
	FallbackProxyURL2 *string        `json:"fallback_proxy_url_2"`
}

// ImportAccountsRequest 是批量导入的请求。
type ImportAccountsRequest struct {
	GroupID       string `json:"group_id"`
	Format        string `json:"format"`
	Content       string `json:"content"`
	OnConflict    string `json:"on_conflict"` // skip | update
	ClientIDFirst *bool  `json:"client_id_first"`
	IMAPHost      string `json:"imap_host"`
	IMAPPort      int    `json:"imap_port"`
	Defaults      struct {
		Remark string        `json:"remark"`
		Status AccountStatus `json:"status"`
	} `json:"defaults"`
}

// ImportError 是导入过程中某一行的失败原因。
type ImportError struct {
	Line   int    `json:"line"`
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// MaxImportErrors 是响应里最多回传的逐行错误数。
// 不截断的话，一份全是坏行的 10 万行文件会撑爆响应体。
const MaxImportErrors = 200

// ImportResult 是批量导入的结果。逐行统计而非全成功/全失败——
// 用户一次粘几千行，因为其中几行有问题就整批回滚，体验极差且浪费上游调用。
type ImportResult struct {
	Total     int           `json:"total"`
	Created   int           `json:"created"`
	Updated   int           `json:"updated"`
	Skipped   int           `json:"skipped"`
	Failed    int           `json:"failed"`
	Errors    []ImportError `json:"errors"`
	Truncated bool          `json:"truncated"`
}

// BatchError 是批量操作中某个账号的失败原因。
type BatchError struct {
	AccountID string `json:"account_id"`
	Reason    string `json:"reason"`
}

// BatchResult 是批量操作的统一返回。
type BatchResult struct {
	Requested int          `json:"requested"`
	Succeeded int          `json:"succeeded"`
	Failed    int          `json:"failed"`
	Errors    []BatchError `json:"errors"`
}

type BatchMoveRequest struct {
	AccountIDs []string `json:"account_ids"`
	GroupID    string   `json:"group_id"`
}

type BatchStatusRequest struct {
	AccountIDs []string      `json:"account_ids"`
	Status     AccountStatus `json:"status"`
}

type BatchProxyRequest struct {
	AccountIDs        []string `json:"account_ids"`
	ProxyURL          string   `json:"proxy_url"`
	FallbackProxyURL1 string   `json:"fallback_proxy_url_1"`
	FallbackProxyURL2 string   `json:"fallback_proxy_url_2"`
}

type BatchDeleteRequest struct {
	AccountIDs []string `json:"account_ids"`
}

// AddError 追加一条逐行错误，超过 MaxImportErrors 后只标记截断、不再累积。
// 一份全是坏行的 10 万行文件会撑爆响应体。
func (r *ImportResult) AddError(line int, email, reason string) {
	if len(r.Errors) >= MaxImportErrors {
		r.Truncated = true
		return
	}
	r.Errors = append(r.Errors, ImportError{Line: line, Email: email, Reason: reason})
}
