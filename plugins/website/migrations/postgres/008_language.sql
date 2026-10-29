-- 官网语言清单（全局部署级配置）：后台可增删，公开站按 prefix 注册路由，保存后重启生效。
-- 首次启动时由插件把 locales/languages.json 写入本表，之后以表为准。
CREATE TABLE plg_website_language (
  id BIGINT PRIMARY KEY,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  code VARCHAR(16) NOT NULL UNIQUE,
  label VARCHAR(64) NOT NULL,
  short VARCHAR(8) NOT NULL DEFAULT '',
  html_lang VARCHAR(16) NOT NULL,
  og_locale VARCHAR(32) NOT NULL DEFAULT '',
  prefix VARCHAR(16) NOT NULL DEFAULT '',
  date_format VARCHAR(32) NOT NULL DEFAULT '',
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  sort INT NOT NULL DEFAULT 0
);
