package model

import "time"

// 操作者身份，对应 audit_logs.actor_kind。与 middleware 里的常量成对，
// 那边是写入侧、这边是查询与展示侧。
const (
	ActorKindUser   = "user"
	ActorKindAdmin  = "admin"
	ActorKindAPIKey = "api_key"
	ActorKindSystem = "system"
)

// 审计动作名。用 `资源.动作` 的形状，前缀相同的能在后台一起筛出来。
//
// 读操作只记管理员的那三类（08 文档 §2.4）：查看账号列表、查看邮件正文、导出账号。
// 普通用户的读不记——一个用户翻十页邮件就是十条，量大到会把真正要看的写操作淹掉。
const (
	AuditAccountList   = "account.list"
	AuditAccountRead   = "account.read"
	AuditAccountCreate = "account.create"
	AuditAccountUpdate = "account.update"
	AuditAccountDelete = "account.delete"
	AuditAccountImport = "account.import"
	AuditAccountBatch  = "account.batch"
	AuditAccountExport = "account.export"

	AuditMessageRead  = "message.read"
	AuditMessageWrite = "message.write"
	AuditGroupWrite   = "group.write"

	AuditAPIKeyReset = "api_key.reset"

	AuditTokenRefresh     = "token.refresh"
	AuditTokenReauthorize = "token.reauthorize"
	AuditJobSubmit        = "job.submit"
	AuditJobStop          = "job.stop"

	AuditUserUpdate        = "user.update"
	AuditUserDelete        = "user.delete"
	AuditUserResetPassword = "user.reset_password"
	AuditPlanCreate        = "plan.create"
	AuditPlanUpdate        = "plan.update"
	AuditPlanDelete        = "plan.delete"
	AuditQuotaUpdate       = "quota.update"
)

// AuditLog 是一条审计记录。
//
// ActorName 是冗余字段：actor_user_id 的外键在用户被删除时会被置空
// （日志本身必须留下），那之后只有这一列还能说明是谁做的。
//
// 存的是**用户名**而不是邮箱。000008 之后邮箱是可选的，用它做这层兜底
// 会在最需要追溯的时候正好是空的；用户名必填且唯一。
type AuditLog struct {
	ID           string    `json:"id"`
	TenantID     string    `json:"tenant_id"`
	ActorUserID  string    `json:"actor_user_id"`
	ActorName    string    `json:"actor_name"`
	ActorKind    string    `json:"actor_kind"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	IP           string    `json:"ip"`
	Details      string    `json:"details"`
	CreatedAt    time.Time `json:"created_at"`
}

// AuditFilter 是审计日志的查询条件，空值表示该项不筛选。
type AuditFilter struct {
	TenantID    string
	ActorUserID string
	ActorKind   string
	Action      string
	Page        int
	Limit       int
}

// Normalize 把翻页参数收敛到合法范围。
// 与 AccountFilter 一样，非法值回落到默认而不是报错——列表页的参数常来自
// 书签或分享链接，因为一个过期参数就返回 400 反而更难用。
func (f *AuditFilter) Normalize() {
	if f.Page < 1 {
		f.Page = 1
	}
	if f.Limit < 1 || f.Limit > 200 {
		f.Limit = 50
	}
}

func (f AuditFilter) Offset() int { return (f.Page - 1) * f.Limit }
