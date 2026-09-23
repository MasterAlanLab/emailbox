UPDATE mail_accounts SET auth_channel = '' WHERE auth_channel = 'imap_gmail';
ALTER TABLE mail_accounts DROP CONSTRAINT IF EXISTS mail_accounts_auth_channel_check;
ALTER TABLE mail_accounts ADD CONSTRAINT mail_accounts_auth_channel_check
    CHECK (auth_channel IN ('', 'graph', 'imap_new', 'imap_old'));
