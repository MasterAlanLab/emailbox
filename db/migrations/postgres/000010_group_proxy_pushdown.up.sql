-- 见 sqlite/000010_group_proxy_pushdown.up.sql 的说明：压平分组之前，
-- 先把「子分组继承父分组代理」这条规则的结果写进数据，避免这批分组下的账号
-- 在结构变更后悄悄从代理出口换成直连。
--
-- 一层一层往下推两次：先让二级继承一级，再让三级继承二级。
-- 重复执行安全：跑过一次之后 proxy_url 已非空，WHERE 不再命中。
UPDATE mail_groups g
SET proxy_url = p.proxy_url,
    fallback_proxy_url_1 = p.fallback_proxy_url_1,
    fallback_proxy_url_2 = p.fallback_proxy_url_2
FROM mail_groups p
WHERE p.id = g.parent_id
  AND g.level = 2
  AND g.proxy_url = ''
  AND p.proxy_url <> '';

UPDATE mail_groups g
SET proxy_url = p.proxy_url,
    fallback_proxy_url_1 = p.fallback_proxy_url_1,
    fallback_proxy_url_2 = p.fallback_proxy_url_2
FROM mail_groups p
WHERE p.id = g.parent_id
  AND g.level = 3
  AND g.proxy_url = ''
  AND p.proxy_url <> '';
