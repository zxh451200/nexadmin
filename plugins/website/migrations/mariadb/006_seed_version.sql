-- 出厂内容补种版本：升级后按版本号补一次新增的出厂新闻，不复活管理员删除的内容。
ALTER TABLE plg_website_setting ADD COLUMN seed_version INT NOT NULL DEFAULT 1;
