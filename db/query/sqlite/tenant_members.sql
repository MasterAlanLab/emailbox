-- name: CreateTenantMember :exec
INSERT INTO tenant_members (id, tenant_id, user_id, role) VALUES (?, ?, ?, ?);
-- name: GetTenantMember :one
SELECT tm.* FROM tenant_members tm JOIN tenants t ON t.id = tm.tenant_id WHERE tm.tenant_id = ? AND tm.user_id = ? AND t.deleted_at IS NULL LIMIT 1;
-- name: ListTenantMembers :many
SELECT tm.id, tm.tenant_id, tm.user_id, tm.role, tm.created_at, tm.updated_at, u.username, u.email FROM tenant_members tm JOIN users u ON u.id = tm.user_id WHERE tm.tenant_id = ? AND u.deleted_at IS NULL ORDER BY tm.created_at ASC;
-- name: UpdateTenantMemberRole :execrows
UPDATE tenant_members SET role = ?, updated_at = CURRENT_TIMESTAMP WHERE tenant_id = ? AND user_id = ?;
-- name: DeleteTenantMemberKeepingOwner :execrows
DELETE FROM tenant_members
WHERE tenant_members.tenant_id = ? AND tenant_members.user_id = ?
  AND (tenant_members.role <> 'owner'
       OR (SELECT COUNT(*) FROM tenant_members m WHERE m.tenant_id = ? AND m.role = 'owner') > 1);
-- name: CountTenantOwners :one
SELECT COUNT(*) FROM tenant_members WHERE tenant_id = ? AND role = 'owner';
