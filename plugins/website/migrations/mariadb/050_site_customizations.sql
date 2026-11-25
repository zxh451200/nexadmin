-- 官网可见性 customization：首页主视觉替换、客服挂件开关、询盘抄送收件人、robots.txt 覆盖。
-- 全部带空/开启默认值：存量站点行为不变（继续用内置截图、挂件照旧按 im 在线显隐、
-- 询盘仍发联系邮箱、robots 仍由服务端生成）。
ALTER TABLE plg_website_setting
  ADD COLUMN `hero_image_url` varchar(500) NOT NULL DEFAULT '',
  ADD COLUMN `hero_image_dark_url` varchar(500) NOT NULL DEFAULT '',
  ADD COLUMN `chat_enabled` TINYINT(1) NOT NULL DEFAULT 1,
  ADD COLUMN `message_recipient_email` varchar(128) NOT NULL DEFAULT '',
  ADD COLUMN `robots_txt` TEXT NULL;
