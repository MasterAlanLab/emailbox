-- 删掉 users.avatar_url。
--
-- 这个项目不做头像：个人资料页只有一个「头像 URL」输入框，填了之后**界面上没有任何
-- 一处会显示它**——左栏、成员列表、后台用户列表用的都是用户名首字。
-- 一个填了不生效的输入框，和 000006 清掉的那批 allow_* 开关是同一类东西。
ALTER TABLE users DROP COLUMN avatar_url;
