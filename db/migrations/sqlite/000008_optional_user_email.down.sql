-- migrate:no-transaction
--
-- 回滚成 email NOT NULL UNIQUE。没设邮箱的用户在旧结构下无处安放
-- （空串只能有一个），因此给他们补一个占位邮箱：no-email+<id>@invalid。
-- .invalid 是 RFC 2606 保留的顶级域，永远不会解析到真实邮箱。
PRAGMA foreign_keys = OFF;

BEGIN;

DROP TABLE IF EXISTS users_old;

CREATE TABLE users_old (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    avatar_url TEXT NOT NULL DEFAULT '',
    status TEXT NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at DATETIME,
    platform_role TEXT NOT NULL DEFAULT 'user'
        CHECK (platform_role IN ('user', 'admin')),
    last_login_at DATETIME
);

INSERT INTO users_old (
    id, username, email, password_hash, avatar_url, status,
    created_at, updated_at, deleted_at, platform_role, last_login_at
)
SELECT id, username,
       CASE WHEN email = '' THEN 'no-email+' || id || '@invalid' ELSE email END,
       password_hash, avatar_url, status,
       created_at, updated_at, deleted_at, platform_role, last_login_at
FROM users;

DROP TABLE users;
ALTER TABLE users_old RENAME TO users;

CREATE INDEX IF NOT EXISTS idx_users_platform_role ON users(platform_role) WHERE platform_role = 'admin';

COMMIT;

PRAGMA foreign_key_check;

PRAGMA foreign_keys = ON;
