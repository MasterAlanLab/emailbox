-- NOTE: keep this file ASCII-only (see pkg/repo/accounts.go for why).

-- name: CreateMailAlias :exec
INSERT INTO mail_account_aliases (id, tenant_id, account_id, alias_email, alias_normalized)
VALUES ($1, $2, $3, $4, $5);

-- name: DeleteMailAliasesByAccount :exec
DELETE FROM mail_account_aliases WHERE tenant_id = $1 AND account_id = $2;

-- Fetch aliases for many accounts in one round trip to avoid N+1.
-- name: ListMailAliasesByAccountIDs :many
SELECT account_id, alias_email FROM mail_account_aliases
WHERE tenant_id = $1 AND account_id = ANY($2::text[])
ORDER BY alias_normalized;

-- name: CountConflictingAliases :one
SELECT COUNT(*) FROM mail_account_aliases
WHERE tenant_id = $1 AND account_id <> $2 AND alias_normalized = ANY($3::text[]);
