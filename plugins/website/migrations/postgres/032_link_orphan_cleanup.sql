-- 清掉悬空的新闻关联行（2026-11-15）：指向已删文章或已删栏目/标签的 plg_website_post_category、
-- plg_website_post_tag 行。来源是主题演示导入的清理漏步（seedDemoTx 删演示文章与栏目时没删它们
-- 的关联行，商品侧一直是先删关联的），每换一次主题就留一批；本仓一个开发库实测到 171 条。
-- 危害：分类与标签的「这个栏目下有多少篇」直接按关联表行数算，悬空行让计数虚高，
-- 公开侧归档页也会数出点不开的文章。
-- 修复同主题的代码在 theme.go（关联先删），这条只管存量库；三引擎同一份语句
-- （MySQL 不接受单表 DELETE 的表别名，所以相关子查询里用全表名限定列）。

DELETE FROM plg_website_post_category
WHERE NOT EXISTS (SELECT 1 FROM plg_website_post p WHERE p.id = plg_website_post_category.post_id)
   OR NOT EXISTS (SELECT 1 FROM plg_website_category c WHERE c.id = plg_website_post_category.category_id);

DELETE FROM plg_website_post_tag
WHERE NOT EXISTS (SELECT 1 FROM plg_website_post p WHERE p.id = plg_website_post_tag.post_id)
   OR NOT EXISTS (SELECT 1 FROM plg_website_tag t WHERE t.id = plg_website_post_tag.tag_id);
