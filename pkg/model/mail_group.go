package model

import "time"

// DefaultGroupName 是注册时自动创建的系统分组名。
const DefaultGroupName = "默认分组"

// GroupColor 是分组的配色。取值受限于 Kumo 的语义令牌（不是十六进制色值），
// 前端直接映射到 Badge 的 variant，见 06 文档 §1.4。
//
// 曾叫 TagColor，因为标签和分组共用这套取值。标签已随 000007 删除，
// 留着那个名字只会让下一个人以为它属于某个还存在的标签功能。
type GroupColor string

const (
	GroupColorBlue   GroupColor = "blue"
	GroupColorGreen  GroupColor = "green"
	GroupColorAmber  GroupColor = "amber"
	GroupColorRed    GroupColor = "red"
	GroupColorPurple GroupColor = "purple"
	GroupColorGray   GroupColor = "gray"
)

// ValidGroupColor 判断颜色是否在允许的令牌集合内。
// 取值集合必须与两个方言迁移里 mail_groups.color 的 CHECK 约束保持一致。
func ValidGroupColor(c GroupColor) bool {
	switch c {
	case GroupColorBlue, GroupColorGreen, GroupColorAmber, GroupColorRed, GroupColorPurple, GroupColorGray:
		return true
	}
	return false
}

// MailGroup 是一个分组。分组是平的一层，没有父子关系——
// 它要解决的问题只是「把账号分堆」，层级带来的规则（选上级、层数上限、
// 直属与含子树两个账号数口径）在这个产品里没有对应的用法。
type MailGroup struct {
	ID          string     `json:"id"`
	TenantID    string     `json:"-"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Color       GroupColor `json:"color"`
	SortOrder   int        `json:"sort_order"`
	IsSystem    bool       `json:"is_system"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// 代理三列构成「主 + 两个备用」。含认证口令，出接口前必须打码。
	ProxyURL          string `json:"-"`
	FallbackProxyURL1 string `json:"-"`
	FallbackProxyURL2 string `json:"-"`
}

// MailGroupNode 是带账号数的分组，供前端左栏直接渲染。
type MailGroupNode struct {
	MailGroup
	// ProxyURLMasked 等三个字段是打码后的代理地址，可以安全地回显。
	ProxyURLMasked          string `json:"proxy_url_masked"`
	FallbackProxyURL1Masked string `json:"fallback_proxy_url_1_masked"`
	FallbackProxyURL2Masked string `json:"fallback_proxy_url_2_masked"`
	// AccountCount 是分组下的账号数。
	AccountCount int `json:"account_count"`
}

type CreateMailGroupRequest struct {
	Name              string     `json:"name"`
	Description       string     `json:"description"`
	Color             GroupColor `json:"color"`
	ProxyURL          string     `json:"proxy_url"`
	FallbackProxyURL1 string     `json:"fallback_proxy_url_1"`
	FallbackProxyURL2 string     `json:"fallback_proxy_url_2"`
}

// UpdateMailGroupRequest 的字段用指针以区分「未提供」（保持原值）与「显式清空」。
type UpdateMailGroupRequest struct {
	Name              *string     `json:"name"`
	Description       *string     `json:"description"`
	Color             *GroupColor `json:"color"`
	ProxyURL          *string     `json:"proxy_url"`
	FallbackProxyURL1 *string     `json:"fallback_proxy_url_1"`
	FallbackProxyURL2 *string     `json:"fallback_proxy_url_2"`
}

type ReorderMailGroupsRequest struct {
	GroupIDs []string `json:"group_ids"`
}
