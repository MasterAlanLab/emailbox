-- 去掉「管理员为单个租户覆盖配额」与调额原因。
--
-- 管理员现在只做一件事：给用户直接分配一个套餐（不经支付），额度一律取套餐本身的值。
-- 逐项覆盖让「这个用户到底有多少额度」要看两处才说得清，而实际需要的只是「给他换一档」。
-- 分配由谁、何时做的，审计日志里有；plan_source / subscription_id 保留，
-- 它们记着套餐是管理员给的还是订阅给的，订阅取消时只回收后者。
ALTER TABLE tenant_quotas DROP COLUMN max_accounts;
ALTER TABLE tenant_quotas DROP COLUMN max_groups;
ALTER TABLE tenant_quotas DROP COLUMN daily_mail_fetch;
ALTER TABLE tenant_quotas DROP COLUMN note;
ALTER TABLE tenant_quotas DROP COLUMN updated_by;
