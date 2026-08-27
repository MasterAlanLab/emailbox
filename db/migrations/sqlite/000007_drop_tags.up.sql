-- 删除标签。整个功能在界面上从来没有闭合过：/mail/tags 能建标签、能删标签，
-- 账号列表和抽屉也会显示 account.tags，但**没有任何地方能把标签贴到账号上**——
-- batchTags 接口没有调用方，导入页也不传 tag_ids。于是标签建了只能躺在列表页里，
-- 账号上的标签永远是空的。这和 000006 清掉的那批预埋物是同一类东西。
--
-- 先删从表：mail_account_tags.tag_id 外键指向 mail_tags，反过来删会被外键拒绝。
-- idx_mail_account_tags_tag 随表一起消失，不必单独 DROP。
DROP TABLE IF EXISTS mail_account_tags;
DROP TABLE IF EXISTS mail_tags;
