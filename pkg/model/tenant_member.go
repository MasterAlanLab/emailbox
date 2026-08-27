package model

import "time"

type TenantMember struct {
	ID        string     `json:"id"`
	TenantID  string     `json:"tenant_id"`
	UserID    string     `json:"user_id"`
	Role      TenantRole `json:"role"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}
type TenantMemberDetail struct {
	TenantMember
	Username string `json:"username"`
	Email    string `json:"email"`
}

// AddMemberRequest 按用户名找人。邮箱自 000008 起可以不填，
// 按邮箱加成员的话，没填邮箱的同事根本没法被加进来。
type AddMemberRequest struct {
	Username string     `json:"username"`
	Role     TenantRole `json:"role"`
}
type UpdateMemberRoleRequest struct {
	Role TenantRole `json:"role"`
}
