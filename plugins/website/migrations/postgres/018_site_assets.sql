-- 站点资产：favicon 与站点默认分享图（og:image 兜底）。此前两者分别写死在模板
-- /website-assets/favicon.svg 与 /website-assets/og.png，交付客户站点换不了。
ALTER TABLE plg_website_setting ADD COLUMN favicon_url VARCHAR(255) NOT NULL DEFAULT '';
ALTER TABLE plg_website_setting ADD COLUMN og_image_url VARCHAR(255) NOT NULL DEFAULT '';
