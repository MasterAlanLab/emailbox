package model

type TenantRole string

const (
	TenantRoleOwner  TenantRole = "owner"
	TenantRoleAdmin  TenantRole = "admin"
	TenantRoleMember TenantRole = "member"
	// TenantRoleAPI 是 API Key 的虚拟角色，只存在于内存里：
	// tenant_members.role 的 CHECK 是 ('owner','admin','member')，
	// 这个取值永远不可能从库里读出来，只能由鉴权中间件在识别出 Key 之后设上。
	//
	// 让 Key 复用角色体系，是为了让它自动受 middleware.Require 约束——
	// /mail/** 那份路由一行不用改，权限收敛就已经生效。
	TenantRoleAPI TenantRole = "api"
)

// PlatformRole 是与租户角色正交的另一个维度：租户角色决定「在某个租户内能做什么」，
// 平台角色决定「能否跨租户管理整个系统」。平台角色不进入下面的 Permission 体系，
// 由 middleware.RequirePlatformAdmin 单独校验。
type PlatformRole string

const (
	PlatformRoleUser  PlatformRole = "user"
	PlatformRoleAdmin PlatformRole = "admin"
)

type Permission string

const (
	PermissionTenantRead   Permission = "tenant:read"
	PermissionTenantUpdate Permission = "tenant:update"
	PermissionTenantDelete Permission = "tenant:delete"
	PermissionMemberRead   Permission = "member:read"
	PermissionMemberCreate Permission = "member:create"
	PermissionMemberUpdate Permission = "member:update"
	PermissionMemberDelete Permission = "member:delete"

	PermissionMailGroupRead  Permission = "mail:group:read"
	PermissionMailGroupWrite Permission = "mail:group:write"
	PermissionAccountRead    Permission = "mail:account:read"
	PermissionAccountWrite   Permission = "mail:account:write"
	PermissionAccountDelete  Permission = "mail:account:delete"
	// PermissionAccountSecret 单独拆出来，是因为「导出账号」等价于导出全部凭据明文——
	// 这是本平台风险最高的操作，必须能独立收敛，并且强制写 audit_logs。
	PermissionAccountSecret Permission = "mail:account:secret"
	PermissionMessageRead   Permission = "mail:message:read"
	PermissionMessageWrite  Permission = "mail:message:write"
	PermissionTokenRefresh  Permission = "mail:token:refresh"
	PermissionAuditRead     Permission = "mail:audit:read"
)

// RolePermissions 是租户角色到权限的映射。个人工作空间模式下用户在自己的租户里恒为 owner，
// 因此拥有全部租户级权限；member/admin 两档是为未来的团队版预留的。
var RolePermissions = map[TenantRole]map[Permission]bool{
	TenantRoleOwner: {
		PermissionTenantRead:     true,
		PermissionTenantUpdate:   true,
		PermissionTenantDelete:   true,
		PermissionMemberRead:     true,
		PermissionMemberCreate:   true,
		PermissionMemberUpdate:   true,
		PermissionMemberDelete:   true,
		PermissionMailGroupRead:  true,
		PermissionMailGroupWrite: true,
		PermissionAccountRead:    true,
		PermissionAccountWrite:   true,
		PermissionAccountDelete:  true,
		PermissionAccountSecret:  true,
		PermissionMessageRead:    true,
		PermissionMessageWrite:   true,
		PermissionTokenRefresh:   true,
		PermissionAuditRead:      true,
	},
	// API Key 只读，且只读取件需要的那三样。
	// 没有 mail:account:secret（导出等于取走全部凭据明文）、没有任何写权限、
	// 也没有 tenant:update——所以 Key 无法读取或重置它自己，泄露后不能自我续命。
	TenantRoleAPI: {
		PermissionMailGroupRead: true,
		PermissionAccountRead:   true,
		PermissionMessageRead:   true,
	},
	TenantRoleAdmin: {
		PermissionTenantRead:     true,
		PermissionTenantUpdate:   true,
		PermissionMemberRead:     true,
		PermissionMemberCreate:   true,
		PermissionMemberUpdate:   true,
		PermissionMemberDelete:   true,
		PermissionMailGroupRead:  true,
		PermissionMailGroupWrite: true,
		PermissionAccountRead:    true,
		PermissionAccountWrite:   true,
		PermissionAccountDelete:  true,
		PermissionAccountSecret:  true,
		PermissionMessageRead:    true,
		PermissionMessageWrite:   true,
		PermissionTokenRefresh:   true,
	},
	TenantRoleMember: {
		PermissionTenantRead:    true,
		PermissionMemberRead:    true,
		PermissionMailGroupRead: true,
		PermissionAccountRead:   true,
		PermissionMessageRead:   true,
	},
}

func HasPermission(role TenantRole, permission Permission) bool {
	return RolePermissions[role][permission]
}
