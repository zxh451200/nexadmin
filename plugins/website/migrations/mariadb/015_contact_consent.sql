-- 在线留言的隐私同意勾选：默认关闭，交付客户的站点在站点设置里打开后，
-- 投稿必须先勾选「已阅读并同意隐私政策」，政策地址指向站内页或外部 https 页。
ALTER TABLE plg_website_setting ADD COLUMN contact_consent TINYINT(1) NOT NULL DEFAULT 0;
ALTER TABLE plg_website_setting ADD COLUMN contact_consent_url VARCHAR(255) NOT NULL DEFAULT '';
