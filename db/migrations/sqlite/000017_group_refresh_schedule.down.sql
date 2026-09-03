-- 索引必须先删：SQLite 拒绝 DROP 掉一个被索引引用的列。
-- 000016 那两列能裸删是因为它们既没有索引也不在任何约束里，这里不是那个情况。
DROP INDEX IF EXISTS idx_mail_groups_next_refresh;
ALTER TABLE mail_groups DROP COLUMN next_refresh_at;
ALTER TABLE mail_groups DROP COLUMN refresh_interval_minutes;
