package model

import "time"

type Session struct {
	ID             string
	UserID         string
	TokenHash      string
	ActiveTenantID *string
	ExpiresAt      time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
type AuthResponse struct {
	User           UserResponse `json:"user"`
	Tenants        []Tenant     `json:"tenants"`
	ActiveTenantID *string      `json:"active_tenant_id"`
	// Desktop 告诉前端自己跑在桌面形态里，目前只用来藏掉「退出」入口——
	// 桌面版的本地账号密码是随机生成、从不展示的，退出之后没人填得出登录框。
	//
	// 搭在这个响应上而不是新开一个端点：前端本来就会在 Layout 挂载时取一次会话，
	// 且守卫会等它返回才渲染，按钮不会先闪一下再消失。
	// 也不用 Cookie——Cookie 不区分端口，桌面版在 127.0.0.1 上种下的标志会被
	// 同一台机器上 Docker 发布在 127.0.0.1:1323 的实例读到，把网页版的入口一起藏掉。
	Desktop bool `json:"desktop"`
}
