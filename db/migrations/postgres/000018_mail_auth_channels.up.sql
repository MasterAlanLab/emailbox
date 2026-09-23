-- 取消 Graph 通道，并增加 Gmail IMAP OAuth 通道。
ALTER TABLE mail_accounts DROP CONSTRAINT IF EXISTS mail_accounts_auth_channel_check;
UPDATE mail_accounts SET auth_channel = '' WHERE auth_channel = 'graph';
ALTER TABLE mail_accounts ADD CONSTRAINT mail_accounts_auth_channel_check
    CHECK (auth_channel IN ('', 'imap_new', 'imap_old', 'imap_gmail', 'imap'));
