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

// 定时刷新的间隔边界，单位分钟。0 不在这个区间里，它单独表示「关闭定时刷新」。
//
// 下限 7 天：Outlook 的 refresh_token 是滑动过期（连续 90 天不用才失效），
// 所以定时刷新要解决的问题只有一个——别让令牌因为长期没人碰而作废。
// 每周碰一次对这件事绰绰有余，再密就纯粹是在给服务商加无谓的量，
// 而量大了会撞风控——风控的表现恰好是一批账号集体认证失败，和令牌真的过期
// 在界面上长得一模一样，用户会朝着完全错误的方向排查。
//
// 上限 30 天：再长就逼近 90 天的失效线，一次失败还没等到下次重试就已经晚了。
const (
	MinRefreshIntervalMinutes = 7 * 24 * 60
	MaxRefreshIntervalMinutes = 30 * 24 * 60
)

// ValidRefreshIntervalMinutes 判断定时刷新间隔是否合法。0 表示关闭。
func ValidRefreshIntervalMinutes(minutes int) bool {
	return minutes == 0 ||
		(minutes >= MinRefreshIntervalMinutes && minutes <= MaxRefreshIntervalMinutes)
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

	// RefreshIntervalMinutes 是定时刷新令牌的间隔，0 表示关闭（默认）。
	// 存间隔而不是 cron 表达式：用户关心的是「多久碰一次令牌」，
	// 而 cron 的「每天 3 点」离开租户时区就没有意义，tenants 表里没有那个概念。
	RefreshIntervalMinutes int `json:"refresh_interval_minutes"`
	// NextRefreshAt 是下次该刷新的时刻，由调度器推进。
	// 它落库而不是从上次任务倒推，理由见 000017 迁移。
	NextRefreshAt *time.Time `json:"next_refresh_at"`
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
	// RefreshIntervalMinutes 传 0 是「关闭定时刷新」，不传是「保持原值」——
	// 这正是这里必须用指针的原因：0 在这个字段上是一个有意义的取值。
	RefreshIntervalMinutes *int `json:"refresh_interval_minutes"`
}

type ReorderMailGroupsRequest struct {
	GroupIDs []string `json:"group_ids"`
}
