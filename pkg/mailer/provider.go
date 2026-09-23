// Package mailer 是邮件协议适配层。
//
// 依赖方向铁律：mailer 不 import repo/service。service 负责「从库里取账号 → 解密 →
// 组装 Credential → 调用 mailer → 把结果写回库」。这样协议层可以独立单测，
// 也可以在没有数据库的情况下用真实账号做联调工具。
package mailer

import "strings"

// OAuth 与 IMAP 端点常量。
const (
	// Microsoft 的旧版 IMAP 通道专用端点。请求体里不带 scope。
	TokenURLLive = "https://login.live.com/oauth20_token.srf"
	// Microsoft 的新版 IMAP 通道端点。
	TokenURLIMAP = "https://login.microsoftonline.com/consumers/oauth2/v2.0/token"
	// Gmail 的 OAuth 令牌端点。
	TokenURLGoogle = "https://oauth2.googleapis.com/token"

	IMAPServerOld   = "outlook.office365.com"
	IMAPServerNew   = "outlook.live.com"
	IMAPServerGmail = "imap.gmail.com"
	DefaultIMAPPort = 993

	ScopeIMAP      = "https://outlook.office.com/IMAP.AccessAsUser.All offline_access"
	ScopeGmailIMAP = "https://mail.google.com/"

	DefaultMicrosoftOAuthClientID = "9e5f94bc-e8a4-4e73-b8be-63364c29d753"
	DefaultOAuthRedirectURI       = "http://localhost:8080"
)

// AccountType 区分「走 OAuth 的 Outlook 账号」与「走 IMAP 的账号」。
// Gmail OAuth 保持 account_type=imap，由 provider 决定使用哪种认证。
type AccountType string

const (
	AccountTypeOutlook AccountType = "outlook"
	AccountTypeIMAP    AccountType = "imap"
)

// Provider 是一个邮件服务商的默认连接参数。
type Provider struct {
	Code     string      `json:"code"`
	Label    string      `json:"label"`
	IMAPHost string      `json:"imap_host"`
	IMAPPort int         `json:"imap_port"`
	Type     AccountType `json:"type"`
}

// ProviderCustom 表示由用户自行填写 IMAP 主机与端口。
const ProviderCustom = "custom"

// Providers 是支持的服务商表。阿里云邮箱已移出内置列表，仍可通过自定义 IMAP 使用。
var Providers = map[string]Provider{
	"outlook":      {Code: "outlook", Label: "Outlook", IMAPHost: IMAPServerNew, IMAPPort: DefaultIMAPPort, Type: AccountTypeOutlook},
	"gmail":        {Code: "gmail", Label: "Gmail", IMAPHost: IMAPServerGmail, IMAPPort: DefaultIMAPPort, Type: AccountTypeIMAP},
	"qq":           {Code: "qq", Label: "QQ邮箱", IMAPHost: "imap.qq.com", IMAPPort: DefaultIMAPPort, Type: AccountTypeIMAP},
	"163":          {Code: "163", Label: "163邮箱", IMAPHost: "imap.163.com", IMAPPort: DefaultIMAPPort, Type: AccountTypeIMAP},
	"126":          {Code: "126", Label: "126邮箱", IMAPHost: "imap.126.com", IMAPPort: DefaultIMAPPort, Type: AccountTypeIMAP},
	"yahoo":        {Code: "yahoo", Label: "Yahoo", IMAPHost: "imap.mail.yahoo.com", IMAPPort: DefaultIMAPPort, Type: AccountTypeIMAP},
	"2925":         {Code: "2925", Label: "2925邮箱", IMAPHost: "imap.2925.com", IMAPPort: DefaultIMAPPort, Type: AccountTypeIMAP},
	ProviderCustom: {Code: ProviderCustom, Label: "自定义 IMAP", IMAPHost: "", IMAPPort: DefaultIMAPPort, Type: AccountTypeIMAP},
}

// domainProvider 把邮箱域名映射到服务商。
var domainProvider = map[string]string{
	"outlook.com": "outlook", "hotmail.com": "outlook", "live.com": "outlook", "live.cn": "outlook",
	"gmail.com": "gmail", "googlemail.com": "gmail",
	"qq.com": "qq", "foxmail.com": "qq",
	"163.com": "163", "126.com": "126",
	"yahoo.com": "yahoo", "yahoo.co.jp": "yahoo", "yahoo.co.uk": "yahoo",
	"2925.com": "2925",
}

// ProviderForEmail 按域名推断服务商，未知域名回落到 custom
// （此时 IMAP 主机与端口需要由调用方显式提供）。
func ProviderForEmail(email string) Provider {
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return Providers[ProviderCustom]
	}
	domain := strings.ToLower(strings.TrimSpace(email[at+1:]))
	if code, ok := domainProvider[domain]; ok {
		return Providers[code]
	}
	return Providers[ProviderCustom]
}

// ProviderByCode 按 code 取服务商，未知 code 回落到 custom。
func ProviderByCode(code string) Provider {
	if p, ok := Providers[strings.ToLower(strings.TrimSpace(code))]; ok {
		return p
	}
	return Providers[ProviderCustom]
}

// KnownProviders 返回按 code 排序的服务商列表，供前端下拉框使用。
func KnownProviders() []Provider {
	order := []string{"outlook", "gmail", "qq", "163", "126", "yahoo", "2925", ProviderCustom}
	out := make([]Provider, 0, len(order))
	for _, code := range order {
		out = append(out, Providers[code])
	}
	return out
}
