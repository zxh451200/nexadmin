-- 语言清单并入平台表 sys_language（见 migrations/framework/system_language.sql）。
-- 官网不再自持一份清单：过去框架读 config.yaml、官网读 plg_website_language，
-- 两份互不可见，加语言只加一边时另一边静默缺。
--
-- 不变式：框架迁移在本次启动的同一趟里刚建好 sys_language，而播种发生在全部迁移之后，
-- 所以此刻它是空的。若它已有同码数据，下面的 INSERT 会撞 code 唯一索引让迁移显式失败——
-- 这是故意的，宁可停下让人核对，也不能悄悄丢掉管理员配过的语言。
INSERT INTO sys_language
  (id, created_at, updated_at, code, name, short, dir, html_lang, og_locale, prefix, date_format, is_default, sort)
SELECT
  id, created_at, updated_at, code, label,
  CASE WHEN short = '' THEN UPPER(SUBSTRING_INDEX(code, '-', 1)) ELSE short END,
  CASE WHEN LOWER(code) REGEXP '^(ar|he|fa|ur|yi|ps|ku)(-|$)' THEN 'rtl' ELSE 'ltr' END,
  html_lang, og_locale, prefix, date_format, is_default, sort
FROM plg_website_language;

DROP TABLE plg_website_language;
