-- 分类标题与标签名唯一：名字就是这两张表的身份——新闻保存时早已按名字复用标签（见 resolveTagsTx），
-- 而分类/标签管理面板此前不查名字，同一个主题能长出第二行（slug 让号成 xxx-2）。同名多行让公开侧
-- 多出指向同一主题的入口，也让选栏目的弹层分不出该点哪一行，所以名字这层要和 slug 一样收进数据库约束。
-- 存量库可能已经有同名行，直接建索引会失败：先把非最小 id 的那些（后来者）身上的新闻关联并到
-- 保留行（同名里 id 最小的那行——建得最早，slug 也最干净，已分享出去的链接更可能指着它），
-- 再删掉后来者，最后建唯一索引。
-- 本文件含数据合并（迁移设计规则里属于会被 WARN 的那类）：不合并就没法在存量库上落这个约束。
-- PostgreSQL 的比较默认区分大小写，索引按 LOWER() 建表达式索引，与服务端判重（LOWER(name) = ?）同一口径。

-- 分类：后来者的新闻关联并到保留行（同一篇已经挂着保留行时 ON CONFLICT DO NOTHING 跳过）
INSERT INTO plg_website_post_category (post_id, category_id)
SELECT pc.post_id, MIN(k.id)
FROM plg_website_post_category pc
JOIN plg_website_category c ON c.id = pc.category_id
JOIN plg_website_category k ON LOWER(k.title) = LOWER(c.title) AND k.id < c.id
GROUP BY pc.post_id, c.id
ON CONFLICT DO NOTHING;

-- 分类：删掉指向后来者的关联（保留行上一步已经接好，这里删了不会丢文章）
DELETE FROM plg_website_post_category pc
USING plg_website_category c, plg_website_category k
WHERE c.id = pc.category_id AND LOWER(k.title) = LOWER(c.title) AND k.id < c.id;

-- 分类：删掉后来者（同名行里 id 不是最小的那些）
DELETE FROM plg_website_category c
USING plg_website_category k
WHERE LOWER(k.title) = LOWER(c.title) AND k.id < c.id;

-- 标签：同一套三步（标签由编辑器打字生长，同名行更容易出现）
INSERT INTO plg_website_post_tag (post_id, tag_id)
SELECT pt.post_id, MIN(k.id)
FROM plg_website_post_tag pt
JOIN plg_website_tag t ON t.id = pt.tag_id
JOIN plg_website_tag k ON LOWER(k.name) = LOWER(t.name) AND k.id < t.id
GROUP BY pt.post_id, t.id
ON CONFLICT DO NOTHING;

DELETE FROM plg_website_post_tag pt
USING plg_website_tag t, plg_website_tag k
WHERE t.id = pt.tag_id AND LOWER(k.name) = LOWER(t.name) AND k.id < t.id;

DELETE FROM plg_website_tag t
USING plg_website_tag k
WHERE LOWER(k.name) = LOWER(t.name) AND k.id < t.id;

CREATE UNIQUE INDEX uk_plg_website_category_title ON plg_website_category (LOWER(title));
CREATE UNIQUE INDEX uk_plg_website_tag_name ON plg_website_tag (LOWER(name));
