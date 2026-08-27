-- 回滚。没有邮箱的用户在旧结构下无处安放（email 是 NOT NULL UNIQUE，
-- 空串只能有一个），因此先给他们补一个占位邮箱，格式 no-email+<id>@invalid。
-- .invalid 是 RFC 2606 保留的顶级域，永远不会解析到真实邮箱。
UPDATE users SET email = 'no-email+' || id || '@invalid' WHERE email = '';

DROP INDEX IF EXISTS idx_users_email;

ALTER TABLE users ALTER COLUMN email DROP DEFAULT;
ALTER TABLE users ADD CONSTRAINT users_email_key UNIQUE (email);
