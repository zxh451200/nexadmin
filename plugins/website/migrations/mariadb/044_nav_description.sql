-- 顶部导航项的一句话说明：二级菜单（大面板）里跟在标题下面的那行小字。
-- 留空即退化为纯链接（面板只显示标题），所以默认空串而不是 NULL。
ALTER TABLE plg_website_nav ADD COLUMN description VARCHAR(200) NOT NULL DEFAULT '';
