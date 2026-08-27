-- NOTE: keep this file ASCII-only (see pkg/repo/accounts.go for why).

-- name: CreateMailAlias :exec
INSERT INTO mail_account_aliases (id, tenant_id, account_id, alias_email, alias_normalized)
VALUES (?, ?, ?, ?, ?);

-- name: DeleteMailAliasesByAccount :exec
DELETE FROM mail_account_aliases WHERE tenant_id = ? AND account_id = ?;

-- Fetch aliases for many accounts in one round trip to avoid N+1.
-- name: ListMailAliasesByAccountIDs :many
SELECT account_id, alias_email FROM mail_account_aliases
WHERE tenant_id = ? AND account_id IN (sqlc.slice(account_ids))
ORDER BY alias_normalized;

-- name: CountConflictingAliases :one
SELECT COUNT(*) FROM mail_account_aliases
WHERE tenant_id = ? AND account_id <> ? AND alias_normalized IN (sqlc.slice(aliases));
