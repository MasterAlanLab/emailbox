package model

import "time"

// APIKeyPrefix 是 Key 的固定前缀。带前缀是为了让它在日志、粘贴板、代码库里
// 一眼可辨——扫描工具也能靠它命中误提交。
const APIKeyPrefix = "ebx_"

// TenantAPIKey 是租户的对外取件凭据，一个租户至多一把。
type TenantAPIKey struct {
	TenantID string
	// TokenHash 是 SHA-256 摘要，鉴权时按它查。
	TokenHash string
	// TokenEnc 是明文的 AES-GCM 密文，只用于页面回显（见 000012 迁移的说明）。
	TokenEnc  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// APIKeyView 是回显给页面的形态。明文只在这里出现，不进任何日志。
type APIKeyView struct {
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}
