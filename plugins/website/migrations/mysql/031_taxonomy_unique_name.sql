-- 分类标题与标签名唯一：名字就是这两张表的身份——新闻保存时早已按名字复用标签（见 resolveTagsTx），
-- 而分类/标签管理面板此前不查名字，同一个主题能长出第二行（slug 让号成 xxx-2）。同名多行让公开侧
-- 多出指向同一主题的入口，也让选栏目的弹层分不出该点哪一行，所以名字这层要和 slug 一样收进数据库约束。
-- 存量库可能已经有同名行，直接建索引会失败：先把非最小 id 的那些（后来者）身上的新闻关联并到
-- 保留行（同名里 id 最小的那行——建得最早，slug 也最干净，已分享出去的链接更可能指着它），
-- 再删掉后来者，最后建唯一索引。
-- 本文件含数据合并（迁移设计规则里属于会被 WARN 的那类）：不合并就没法在存量库上落这个约束。
-- MySQL/MariaDB 这两列的排序规则（utf8mb4_general_ci）本身忽略大小写，UNIQUE(title)/UNIQUE(name)
-- 就等价于按名字不区分大小写判重；只有把列换成二进制排序规则的库才会比服务端更松，那种库里
-- 应用层的判重仍是更严的一道（见 taxonomy.go 的 tagNameTaken）。

-- 分类：后来者的新闻关联并到保留行（同一篇已经挂着保留行时，INSERT IGNORE 自动跳过）
INSERT IGNORE INTO plg_website_post_category (post_id, category_id)
SELECT pc.post_id, MIN(k.id)
FROM plg_website_post_category pc
JOIN plg_website_category c ON c.id = pc.category_id
JOIN plg_website_category k ON k.title = c.title AND k.id < c.id
GROUP BY pc.post_id, c.id;

-- 分类：删掉指向后来者的关联（保留行上一步已经接好，这里删了不会丢文章）
DELETE pc FROM plg_website_post_category pc
JOIN plg_website_category c ON c.id = pc.category_id
JOIN plg_website_category k ON k.title = c.title AND k.id < c.id;

-- 分类：删掉后来者（同名行里 id 不是最小的那些）
DELETE c FROM plg_website_category c
JOIN plg_website_category k ON k.title = c.title AND k.id < c.id;

-- 标签：同一套三步（标签由编辑器打字生长，同名行更容易出现）
INSERT IGNORE INTO plg_website_post_tag (post_id, tag_id)
SELECT pt.post_id, MIN(k.id)
FROM plg_website_post_tag pt
JOIN plg_website_tag t ON t.id = pt.tag_id
JOIN plg_website_tag k ON k.name = t.name AND k.id < t.id
GROUP BY pt.post_id, t.id;

DELETE pt FROM plg_website_post_tag pt
JOIN plg_website_tag t ON t.id = pt.tag_id
JOIN plg_website_tag k ON k.name = t.name AND k.id < t.id;

DELETE t FROM plg_website_tag t
JOIN plg_website_tag k ON k.name = t.name AND k.id < t.id;

ALTER TABLE plg_website_category ADD UNIQUE KEY uk_plg_website_category_title (title);
ALTER TABLE plg_website_tag ADD UNIQUE KEY uk_plg_website_tag_name (name);
