package model

import "testing"

// 权限矩阵是租户隔离之外的第二道闸门，改动必须是有意识的。
// 这张表对应 docs/plan/03-data-model.md §8 的角色映射。
func TestRolePermissionMatrix(t *testing.T) {
	cases := []struct {
		permission Permission
		owner      bool
		admin      bool
		member     bool
	}{
		{PermissionMailGroupRead, true, true, true},
		{PermissionAccountRead, true, true, true},
		{PermissionMessageRead, true, true, true},

		{PermissionMailGroupWrite, true, true, false},
		{PermissionAccountWrite, true, true, false},
		{PermissionMessageWrite, true, true, false},
		{PermissionAccountDelete, true, true, false},
		{PermissionTokenRefresh, true, true, false},
		{PermissionAccountSecret, true, true, false},

		{PermissionAuditRead, true, false, false},
	}
	for _, c := range cases {
		for role, want := range map[TenantRole]bool{
			TenantRoleOwner:  c.owner,
			TenantRoleAdmin:  c.admin,
			TenantRoleMember: c.member,
		} {
			if got := HasPermission(role, c.permission); got != want {
				t.Errorf("HasPermission(%s, %s) = %v，期望 %v", role, c.permission, got, want)
			}
		}
	}
}

func TestUnknownRoleAndPermissionDenied(t *testing.T) {
	if HasPermission(TenantRole("guest"), PermissionAccountRead) {
		t.Error("未知角色不应拥有任何权限")
	}
	if HasPermission(TenantRoleOwner, Permission("mail:nonexistent")) {
		t.Error("未定义的权限不应被放行")
	}
}
