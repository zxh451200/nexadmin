-- 官网默认语言：Setting.DefaultLang 指向占根路径的语言 code；空 = auto（按浏览器语言协商）。
-- 内容原文语言锚点（平台默认语言）不受影响；公开站路由按启动时的语言表注册，改完需重启。
ALTER TABLE plg_website_setting ADD COLUMN default_lang VARCHAR(16) NOT NULL DEFAULT '';
