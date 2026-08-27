package model

import "time"

type UserStatus string

const (
	UserStatusActive   UserStatus = "active"
	UserStatusDisabled UserStatus = "disabled"
)

type User struct {
	ID           string     `json:"id"`
	Username     string     `json:"username"`
	Email        string     `json:"email"`
	PasswordHash string     `json:"-"`
	Status       UserStatus `json:"status"`
	// PlatformRole 与租户角色正交：它决定能否跨租户管理整个系统。
	PlatformRole PlatformRole `json:"platform_role"`
	CreatedAt    time.Time    `json:"created_at"`
	UpdatedAt    time.Time    `json:"updated_at"`
	DeletedAt    *time.Time   `json:"-"`
}
type UserResponse struct {
	ID           string       `json:"id"`
	Username     string       `json:"username"`
	Email        string       `json:"email"`
	Status       UserStatus   `json:"status"`
	PlatformRole PlatformRole `json:"platform_role"`
}

func (u User) ToResponse() UserResponse {
	return UserResponse{ID: u.ID, Username: u.Username, Email: u.Email, Status: u.Status, PlatformRole: u.PlatformRole}
}

// IsPlatformAdmin 表示该用户是否为平台管理员。
func (u User) IsPlatformAdmin() bool { return u.PlatformRole == PlatformRoleAdmin }

// RegisterRequest 只要用户名和密码。Email 留空即可，登录后在个人资料里补，
// 或者一直不填——它不参与登录，也不参与任何必需流程。
type RegisterRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginRequest 用**用户名**做登录身份。邮箱自 000008 起是可选的资料字段，
// 一个可以不填的字段当不了凭据——否则「没设邮箱的人怎么登录」就没有答案。
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}
type UpdateProfileRequest struct {
	Username string `json:"username"`
	// Email 用指针以区分「字段未提供」（nil，保持原值）和「显式清空」（""）。
	// 邮箱自 000008 起可选，用户必须能把它删掉——用 string 的话空串会被当成
	// 「没传」，设过一次就再也清不掉了。
	Email *string `json:"email"`
}
type ChangePasswordRequest struct {
	OldPassword string `json:"old_password"`
	NewPassword string `json:"new_password"`
}
