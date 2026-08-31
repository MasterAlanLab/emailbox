-- 分组的代理编辑框只保留一个地址，不再区分主/备用（账号自己的代理不受影响，
-- 仍是三列——见 04 文档 §6）。SQLite 3.35+ 原生支持 DROP COLUMN，这两列既不在
-- UNIQUE(tenant_id, name) 里也没有索引或外键，不需要 000011 那种整表重建。
ALTER TABLE mail_groups DROP COLUMN fallback_proxy_url_1;
ALTER TABLE mail_groups DROP COLUMN fallback_proxy_url_2;
