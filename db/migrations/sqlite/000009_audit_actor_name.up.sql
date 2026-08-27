-- audit_logs.actor_email 改名 actor_name，存用户名而不是邮箱。
--
-- 这个字段的用途写在 000004 里：actor_user_id 的外键在用户被删除时会被
-- ON DELETE SET NULL 置空，冗余存一份操作者标识，光看 NULL 才不至于无从追溯是谁。
-- 邮箱在 000008 之后变成可选，这个冗余字段会经常是空的——恰恰在最需要追溯的
-- 时候没有值，等于这层保护形同虚设。用户名必填且唯一，是现在唯一能一直
-- 履行这个职责的标识。
ALTER TABLE audit_logs RENAME COLUMN actor_email TO actor_name;
