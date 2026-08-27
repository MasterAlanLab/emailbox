-- NOTE: keep this file ASCII-only. sqlc miscomputes query boundaries when a
-- .sql file contains multi-byte characters and silently truncates the SQL.
-- Explanations live in pkg/repo/accounts.go instead.
--
-- Variable-length IN lists use sqlc.slice(), which sqlc expands to IN (?, ?, ?) at call time. The two dialects deliberately
-- diverge -- repo method signatures stay identical and pkg/repo/parity_test.go
-- guards against drift.


-- name: CreateMailAccount :exec
INSERT INTO mail_accounts (
    id, tenant_id, group_id, email, email_normalized, provider, account_type,
    password_enc, client_id, refresh_token_enc, imap_host, imap_port, imap_password_enc,
    status, remark, sort_order, proxy_url, fallback_proxy_url_1, fallback_proxy_url_2
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?);

-- name: GetMailAccount :one
SELECT * FROM mail_accounts
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL LIMIT 1;

-- name: GetMailAccountByEmail :one
SELECT * FROM mail_accounts
WHERE tenant_id = ? AND email_normalized = ? AND deleted_at IS NULL LIMIT 1;

-- name: ListMailAccountsByIDs :many
SELECT * FROM mail_accounts
WHERE tenant_id = ? AND deleted_at IS NULL
  AND id IN (sqlc.slice(account_ids))
ORDER BY sort_order, created_at DESC;

-- name: CountMailAccounts :one
SELECT COUNT(*) FROM mail_accounts WHERE tenant_id = ? AND deleted_at IS NULL;

-- name: UpdateMailAccount :execrows
UPDATE mail_accounts
SET group_id = ?,
    provider = ?,
    account_type = ?,
    password_enc = ?,
    client_id = ?,
    refresh_token_enc = ?,
    imap_host = ?,
    imap_port = ?,
    imap_password_enc = ?,
    status = ?,
    remark = ?,
    proxy_url = ?,
    fallback_proxy_url_1 = ?,
    fallback_proxy_url_2 = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL;

-- Soft delete clears the three credential columns in the same statement.
-- Credential ciphertext must not linger behind a deleted_at flag.
-- name: SoftDeleteMailAccount :execrows
UPDATE mail_accounts
SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP,
    password_enc = '', refresh_token_enc = '', imap_password_enc = ''
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL;

-- name: BatchMoveMailAccounts :execrows
UPDATE mail_accounts
SET group_id = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND deleted_at IS NULL AND id IN (sqlc.slice(account_ids));

-- name: BatchUpdateMailAccountStatus :execrows
UPDATE mail_accounts
SET status = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND deleted_at IS NULL AND id IN (sqlc.slice(account_ids));

-- name: BatchUpdateMailAccountProxy :execrows
UPDATE mail_accounts
SET proxy_url = ?, fallback_proxy_url_1 = ?, fallback_proxy_url_2 = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND deleted_at IS NULL AND id IN (sqlc.slice(account_ids));

-- name: BatchSoftDeleteMailAccounts :execrows
UPDATE mail_accounts
SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP,
    password_enc = '', refresh_token_enc = '', imap_password_enc = ''
WHERE tenant_id = ? AND deleted_at IS NULL AND id IN (sqlc.slice(account_ids));

-- Optional filters use the "NULL means ignore" pattern. tenant_id and
-- deleted_at are constant conditions and come first so the composite indexes
-- can still help.
--
-- Sorting is spelled out as one query per (sort, direction) pair because sqlc
-- cannot parameterise ORDER BY at all -- sqlc.arg() inside ORDER BY is left as
-- literal text in the emitted SQL, and a bare ? there is silently dropped.
-- Both failures only surface at runtime. The service maps the API's
-- sort/order pair onto these; the shared WHERE clause is identical in every
-- variant, and parity tests cover the pairs.
-- name: CountMailAccountsFiltered :one
SELECT COUNT(*) FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0);

-- name: ListMailAccountsPageBySortOrderAsc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY sort_order ASC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: ListMailAccountsPageBySortOrderDesc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY sort_order DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: ListMailAccountsPageByEmailAsc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY email_normalized ASC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: ListMailAccountsPageByEmailDesc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY email_normalized DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: ListMailAccountsPageByCreatedAtAsc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY created_at ASC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: ListMailAccountsPageByCreatedAtDesc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY created_at DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: ListMailAccountsPageByLastRefreshAtAsc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY last_refresh_at ASC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- name: ListMailAccountsPageByLastRefreshAtDesc :many
SELECT * FROM mail_accounts
WHERE tenant_id = sqlc.arg(tenant_id)
  AND deleted_at IS NULL
  AND (sqlc.narg(status) IS NULL OR status = sqlc.narg(status))
  AND (sqlc.narg(refresh_status) IS NULL OR last_refresh_status = sqlc.narg(refresh_status))
  AND (sqlc.narg(provider) IS NULL OR provider = sqlc.narg(provider))
  AND (sqlc.narg(group_id) IS NULL OR group_id = sqlc.narg(group_id))
  AND (sqlc.narg(q) IS NULL
       OR instr(email_normalized, sqlc.narg(q)) > 0
       OR instr(lower(remark), sqlc.narg(q)) > 0)
ORDER BY last_refresh_at DESC, id
LIMIT sqlc.arg(row_limit) OFFSET sqlc.arg(row_offset);

-- Hot path: clicking a group in the left pane. Skips every optional clause so
-- idx_mail_accounts_group applies cleanly.
-- name: UpdateMailAccountAuthChannel :execrows
UPDATE mail_accounts
SET auth_channel = ?, updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL;

-- Microsoft rotates refresh tokens. Missing a rotation breaks the account on
-- the next refresh, so this must be its own statement that cannot be skipped.
-- name: UpdateMailAccountRefreshToken :execrows
UPDATE mail_accounts
SET refresh_token_enc = ?,
    refresh_token_updated_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL;

-- name: UpdateMailAccountRefreshResult :execrows
UPDATE mail_accounts
SET last_refresh_at = CURRENT_TIMESTAMP,
    last_refresh_status = ?,
    last_refresh_error = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE tenant_id = ? AND id = ? AND deleted_at IS NULL;

-- Used when an admin deletes a user: wipes the whole tenant's mailboxes and
-- clears the credential ciphertext in the same statement, so nothing sensitive
-- lingers behind a deleted_at flag (08 doc section 6, item 6).
-- name: SoftDeleteMailAccountsByTenant :execrows
UPDATE mail_accounts
SET deleted_at = CURRENT_TIMESTAMP, updated_at = CURRENT_TIMESTAMP,
    password_enc = '', refresh_token_enc = '', imap_password_enc = ''
WHERE tenant_id = ? AND deleted_at IS NULL;
