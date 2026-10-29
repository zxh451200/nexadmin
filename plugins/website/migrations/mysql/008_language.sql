-- 官网语言清单（全局部署级配置）：后台可增删，公开站按 prefix 注册路由，保存后重启生效。
-- 首次启动时由插件把 locales/languages.json 写入本表，之后以表为准。
CREATE TABLE plg_website_language (
  id BIGINT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  code VARCHAR(16) NOT NULL,
  label VARCHAR(64) NOT NULL,
  short VARCHAR(8) NOT NULL DEFAULT '',
  html_lang VARCHAR(16) NOT NULL,
  og_locale VARCHAR(32) NOT NULL DEFAULT '',
  prefix VARCHAR(16) NOT NULL DEFAULT '',
  date_format VARCHAR(32) NOT NULL DEFAULT '',
  is_default TINYINT(1) NOT NULL DEFAULT 0,
  sort INT NOT NULL DEFAULT 0,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_website_language_code (code)
);
