-- 邮箱从「注册必填」降级为「登录后可填、也可以不填」的资料字段。
-- 登录身份随之从邮箱换成用户名——一个可选的字段不能当凭据，
-- 否则「没设邮箱的人怎么登录」就没有答案了。
--
-- users.email 的 NOT NULL 保留（空串表示未设置），但表级 UNIQUE 必须去掉：
-- 它只允许一个人是空串，第二个不填邮箱的人就注册不了。
-- 换成部分唯一索引，只约束真正填了邮箱的行。
--
-- PostgreSQL 可以直接摘掉约束，不需要像 SQLite 那样整表重建。
-- users_email_key 是 CREATE TABLE 里写 `email VARCHAR(255) NOT NULL UNIQUE`
-- 时自动生成的约束名（<表>_<列>_key）。
ALTER TABLE users DROP CONSTRAINT users_email_key;
ALTER TABLE users ALTER COLUMN email SET DEFAULT '';

CREATE UNIQUE INDEX idx_users_email ON users(email) WHERE email <> '';
