-- 分组要压平成单层（结构变更在 000011），这一步先把代理配置的继承关系落到数据里。
--
-- 旧规则是「子分组没配代理就用父分组的」（resolveProxy 逐级向上找）。压平之后
-- 父子关系没了，这批子分组会从「走父分组的代理」变成「直连」——账号还在，
-- 却悄悄换了出口 IP，轻则被风控，重则暴露真实地址。所以先把生效的那一份写下来。
--
-- 三列是一个整体：选中哪一层，就把那一层的主代理和两个备用一起搬过来
-- （见 mailer.ResolveProxy——不允许「主用子的、备用用父的」这种混搭）。
--
-- 最多三级，所以一层一层往下推两次即可：先让二级继承一级，再让三级继承二级
-- （此时二级已经拿到了值，等价于三级向上找到最近的非空祖先）。
-- 重复执行是安全的：跑过一次之后 proxy_url 已经非空，WHERE 不再命中。
UPDATE mail_groups
SET proxy_url = (SELECT p.proxy_url FROM mail_groups p WHERE p.id = mail_groups.parent_id),
    fallback_proxy_url_1 = (SELECT p.fallback_proxy_url_1 FROM mail_groups p WHERE p.id = mail_groups.parent_id),
    fallback_proxy_url_2 = (SELECT p.fallback_proxy_url_2 FROM mail_groups p WHERE p.id = mail_groups.parent_id)
WHERE level = 2
  AND proxy_url = ''
  AND EXISTS (SELECT 1 FROM mail_groups p WHERE p.id = mail_groups.parent_id AND p.proxy_url <> '');

UPDATE mail_groups
SET proxy_url = (SELECT p.proxy_url FROM mail_groups p WHERE p.id = mail_groups.parent_id),
    fallback_proxy_url_1 = (SELECT p.fallback_proxy_url_1 FROM mail_groups p WHERE p.id = mail_groups.parent_id),
    fallback_proxy_url_2 = (SELECT p.fallback_proxy_url_2 FROM mail_groups p WHERE p.id = mail_groups.parent_id)
WHERE level = 3
  AND proxy_url = ''
  AND EXISTS (SELECT 1 FROM mail_groups p WHERE p.id = mail_groups.parent_id AND p.proxy_url <> '');
