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

	// 代理地址。含认证口令，出接口前必须打码。
	ProxyURL string `json:"-"`
}

// MailGroupNode 是带账号数的分组，供前端左栏直接渲染。
type MailGroupNode struct {
	MailGroup
	// ProxyURLMasked 是打码后的代理地址，可以安全地回显。
	ProxyURLMasked string `json:"proxy_url_masked"`
	// AccountCount 是分组下的账号数。
	AccountCount int `json:"account_count"`
}

// MailGroupProxy 是分组代理的**明文**，只由 GET /mail/groups/:groupID/proxy 返回。
//
// 它存在的理由只有一个：编辑表单要回填。回填打码串的话，用户改完名字一按保存，
// "socks5://u:****@host" 就被当作口令原样存回去，代理从此是坏的——而界面上
// 看起来一切正常，直到某个账号取信失败才会发现。
//
// 这是分组这边唯一一个把凭据明文送出接口的地方，因此按导出的同一档收敛：
// account:secret 权限 + 强制审计，两件都挂在 api/routes.go 的路由上。
type MailGroupProxy struct {
	ProxyURL string `json:"proxy_url"`
}

type CreateMailGroupRequest struct {
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Color       GroupColor `json:"color"`
	ProxyURL    string     `json:"proxy_url"`
}

// UpdateMailGroupRequest 的字段用指针以区分「未提供」（保持原值）与「显式清空」。
type UpdateMailGroupRequest struct {
	Name        *string     `json:"name"`
	Description *string     `json:"description"`
	Color       *GroupColor `json:"color"`
	ProxyURL    *string     `json:"proxy_url"`
}

type ReorderMailGroupsRequest struct {
	GroupIDs []string `json:"group_ids"`
}
