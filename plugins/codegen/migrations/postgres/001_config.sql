-- 代码生成器的保存配置：向导里存一份（表/定义 + 列配置），下次载入继续改。
-- 只存配置不存生成物，代码永远现场渲染。
CREATE TABLE plg_codegen_config (
  id BIGINT PRIMARY KEY,
  name VARCHAR(64) NOT NULL,
  module VARCHAR(32) NOT NULL,
  go_module VARCHAR(100) NOT NULL DEFAULT '',
  table_name VARCHAR(64) NOT NULL DEFAULT '',
  define_json TEXT,
  columns_json TEXT,
  sub_json TEXT,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_codegen_config_name ON plg_codegen_config (name);
