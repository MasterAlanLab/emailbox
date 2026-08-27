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
}
