-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/api_keys.go instead.

-- name: GetAPIKeyByTenant :one
SELECT * FROM tenant_api_keys WHERE tenant_id = ? LIMIT 1;

-- name: GetAPIKeyByHash :one
SELECT * FROM tenant_api_keys WHERE token_hash = ? LIMIT 1;

-- name: UpsertAPIKey :exec
INSERT INTO tenant_api_keys (tenant_id, token_hash, token_enc)
VALUES (?, ?, ?)
ON CONFLICT (tenant_id) DO UPDATE
SET token_hash = excluded.token_hash,
    token_enc = excluded.token_enc,
    updated_at = CURRENT_TIMESTAMP;
