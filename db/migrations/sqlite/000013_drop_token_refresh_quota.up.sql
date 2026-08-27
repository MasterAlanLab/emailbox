-- 取消「每日刷新令牌」的额度。
--
-- 刷新令牌是「账号还能不能用」的前提：卡住它，用户看到的不是「今天少刷一点」，
-- 而是一批账号集体登录失败——那个后果比省下的上游调用重得多。而真正需要防的
-- 「批量刷把服务商打到风控」，靠的是任务系统里的并发数与账号间隔（JOB_WORKERS /
-- JOB_ACCOUNT_DELAY_MS），不是一个每天清零的计数上限。
--
-- usage_counters 里的 token_refresh 仍然照常累加，只是不再有人拿它去判上限——
-- 用量页上那个数字是「是不是有脚本在空转」的唯一线索，留着。
ALTER TABLE plans DROP COLUMN daily_token_refresh;
ALTER TABLE tenant_quotas DROP COLUMN daily_token_refresh;
