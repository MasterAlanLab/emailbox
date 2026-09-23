-- 回滚会把 Gmail 通道记录清空为未记录；旧版本没有可用的等价通道。
UPDATE mail_accounts SET auth_channel = '' WHERE auth_channel = 'imap_gmail';
