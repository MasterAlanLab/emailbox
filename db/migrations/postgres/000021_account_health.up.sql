-- 账号健康状态：令牌有效不等于账号可用。
--
-- last_refresh_* 由令牌刷新与收信共用，一次成功的令牌刷新就会把它改回 success，
-- 可线上确有一批账号令牌刷新永远成功、邮箱却拒绝连接。账号能不能用必须另起一列，
-- 只由「真正登录过邮箱」的结果（账号检测任务、收信）写入，令牌刷新只能把它改成 invalid。
--
-- unknown：从没登录过邮箱；ok：最近一次登录成功；
-- invalid：账号自身确定不可用（封禁、邮箱拒绝连接、授权失效），批量清理只删这一类；
-- error：最近一次没能得出结论（网络、代理、限流），不参与清理。
ALTER TABLE mail_accounts ADD COLUMN health_status VARCHAR(20) NOT NULL DEFAULT 'unknown'
    CHECK (health_status IN ('unknown', 'ok', 'invalid', 'error'));
ALTER TABLE mail_accounts ADD COLUMN health_error_kind VARCHAR(40) NOT NULL DEFAULT '';
ALTER TABLE mail_accounts ADD COLUMN health_error TEXT NOT NULL DEFAULT '';
ALTER TABLE mail_accounts ADD COLUMN health_checked_at TIMESTAMPTZ;

-- 存量里已经能下结论的两类直接回填，升级后不必先跑一遍检测才看得到。
-- 邮箱拒绝连接的那批此前被误归为 auth_failed，这里一并改正分类。
UPDATE mail_accounts
SET health_status = 'invalid', health_error_kind = 'banned',
    health_error = last_refresh_error, health_checked_at = last_refresh_at
WHERE status = 'banned';

UPDATE mail_accounts
SET health_status = 'invalid', health_error_kind = 'account_unavailable',
    health_error = last_refresh_error, health_checked_at = last_refresh_at,
    last_refresh_error_kind = 'account_unavailable'
WHERE status <> 'banned'
  AND last_refresh_status = 'failed'
  AND strpos(lower(last_refresh_error), 'user is authenticated but not connected') > 0;

CREATE INDEX idx_mail_accounts_health ON mail_accounts(tenant_id, health_status);
