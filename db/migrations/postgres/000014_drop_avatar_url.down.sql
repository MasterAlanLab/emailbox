-- 只恢复结构，不恢复数据。
ALTER TABLE users ADD COLUMN avatar_url TEXT NOT NULL DEFAULT '';
