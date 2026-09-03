-- 分组级的定期令牌刷新。说明见 sqlite 侧的同名文件。
ALTER TABLE mail_groups ADD COLUMN refresh_interval_minutes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE mail_groups ADD COLUMN next_refresh_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_mail_groups_next_refresh
    ON mail_groups(next_refresh_at) WHERE refresh_interval_minutes > 0;
