-- 设为首页：Setting.HomeSlug 指向一张已发布单页，首页改为渲染该页内容（替换内置 Hero 页）；
-- 页面本身仍可经 /p/:slug 访问。只存 slug：页面被删或下线时首页自动回退内置首页，不留悬空 ID。
ALTER TABLE plg_website_setting ADD COLUMN home_slug VARCHAR(64) NOT NULL DEFAULT '';
