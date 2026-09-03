-- 分组级的定期令牌刷新。
--
-- refresh_interval_minutes = 0 表示不定时刷新，也是全部存量分组升级后的取值：
-- 这个功能开箱即关。默认打开的话，升级完的第一分钟就会替所有用户往微软打一批
-- 令牌请求，而他们并没有要求过这件事。
--
-- next_refresh_at 落库，不从「上次任务的时间 + 间隔」倒推。倒推要先列租户、
-- 再列分组、再查每个分组最后一个任务（N+1），而且任务被保留期清掉之后就再也
-- 算不出来了——那时所有分组会同时表现为「从没刷过，立刻该刷」。
ALTER TABLE mail_groups ADD COLUMN refresh_interval_minutes INTEGER NOT NULL DEFAULT 0;
ALTER TABLE mail_groups ADD COLUMN next_refresh_at DATETIME;

-- 调度器每分钟扫一次这个索引。带 WHERE 收成部分索引是因为绝大多数分组不开定时：
-- 扫描代价因此与「开了定时的分组数」成正比，而不是与租户总分组数成正比。
CREATE INDEX IF NOT EXISTS idx_mail_groups_next_refresh
    ON mail_groups(next_refresh_at) WHERE refresh_interval_minutes > 0;
