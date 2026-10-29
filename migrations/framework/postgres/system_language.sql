-- 平台语言清单：全站唯一的语言真源。后台可增删改，首次启动时由 core 用 config.yaml 的 languages 播种，之后以表为准。
-- prefix 是公开站的 URL 路径前缀（默认语言必须为空，渲染在无前缀路径）；dir 供前端设 document.dir。
-- 表内没有「启用」开关：出现在清单里就是启用，与既有语义一致。
CREATE TABLE sys_language (
  id BIGINT PRIMARY KEY,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL,
  code VARCHAR(16) NOT NULL UNIQUE,
  name VARCHAR(64) NOT NULL,
  short VARCHAR(8) NOT NULL DEFAULT '',
  dir VARCHAR(3) NOT NULL DEFAULT 'ltr',
  html_lang VARCHAR(16) NOT NULL,
  og_locale VARCHAR(32) NOT NULL DEFAULT '',
  prefix VARCHAR(16) NOT NULL DEFAULT '',
  date_format VARCHAR(32) NOT NULL DEFAULT '',
  is_default BOOLEAN NOT NULL DEFAULT FALSE,
  sort INT NOT NULL DEFAULT 0
);
