-- 分组的代理编辑框只保留一个地址，不再区分主/备用（账号自己的代理不受影响，
-- 仍是三列——见 04 文档 §6）。
ALTER TABLE mail_groups DROP COLUMN fallback_proxy_url_1;
ALTER TABLE mail_groups DROP COLUMN fallback_proxy_url_2;
