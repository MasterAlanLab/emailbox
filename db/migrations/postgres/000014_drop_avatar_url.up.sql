-- 删掉 users.avatar_url，理由见 sqlite/000014_drop_avatar_url.up.sql。
ALTER TABLE users DROP COLUMN IF EXISTS avatar_url;
