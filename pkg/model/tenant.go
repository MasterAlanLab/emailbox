package model

import "time"

// TenantKind 区分个人工作空间与团队空间。个人空间由注册流程自动创建，
// 不可删除、不可添加成员、不可改 slug——删掉它会让账号变成无处可去的孤儿。
type TenantKind string

const (
	TenantKindPersonal TenantKind = "personal"
	TenantKindTeam     TenantKind = "team"
)

type Tenant struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Slug      string     `json:"slug"`
	Kind      TenantKind `json:"kind"`
	CreatedBy string     `json:"created_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"-"`
}
type CreateTenantRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}
type UpdateTenantRequest struct {
	Name string `json:"name"`
	Slug string `json:"slug"`
}
