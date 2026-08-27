-- migrate:no-transaction
--
-- 邮箱从「注册必填」降级为「登录后可填、也可以不填」的资料字段。
-- 登录身份随之从邮箱换成用户名——一个可选的字段不能当凭据，
-- 否则「没设邮箱的人怎么登录」就没有答案。
--
-- users.email 的 NOT NULL 保留（空串表示未设置，与本项目其余可空文本一致），
-- 但表级 UNIQUE 必须去掉：它只允许**一个**人是空串，第二个不填邮箱的人就注册不了。
-- 换成部分唯一索引，只约束真正填了邮箱的行。
--
-- SQLite 去不掉写在列上的 UNIQUE，只能整表重建；重建要 DROP 旧表，而
-- tenants / tenant_members / sessions / audit_logs 四张表都有外键指着 users。
-- 官方做法是先关掉外键检查，但那个 PRAGMA 在事务内是空操作，所以这个文件
-- 用 migrate:no-transaction 自己管事务（见 migrate.go 的 NoTxDirective）。
--
-- 该指令要求文件可重复执行：版本号是在文件跑完之后才记的，两者之间崩溃会重跑。
-- 这里的每一步都满足——重跑时 users 已是新结构，照样能原样再重建一遍。
PRAGMA foreign_keys = OFF;

BEGIN;

-- 上一次跑到一半崩在这里的话，users_new 会留下来挡住 CREATE。
DROP TABLE IF EXISTS users_new;

CREATE TABLE users_new (
    id TEXT PRIMARY KEY,
    username TEXT NOT NULL UNIQUE,
    email TEXT NOT NULL DEFAULT '',
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

-- 显式列出列名，不用 SELECT *：users 的列顺序在 000002 与 000004 各追加过一次，
-- 靠位置对齐的话，以后任何一次 ALTER 都会让这条 INSERT 静默串列。
INSERT INTO users_new (
    id, username, email, password_hash, avatar_url, status,
    created_at, updated_at, deleted_at, platform_role, last_login_at
)
SELECT id, username, email, password_hash, avatar_url, status,
       created_at, updated_at, deleted_at, platform_role, last_login_at
FROM users;

DROP TABLE users;
ALTER TABLE users_new RENAME TO users;

-- 整表重建会连 idx_users_platform_role 一起带走，重新建出来。
CREATE INDEX IF NOT EXISTS idx_users_platform_role ON users(platform_role) WHERE platform_role = 'admin';
CREATE UNIQUE INDEX IF NOT EXISTS idx_users_email ON users(email) WHERE email <> '';

COMMIT;

-- 重建后再核一次：外键是否仍然全部成立。不通过的话这里会直接报错，
-- 好过让一个引用断裂的库继续跑下去。
PRAGMA foreign_key_check;

PRAGMA foreign_keys = ON;
