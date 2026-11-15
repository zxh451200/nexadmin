-- 阿里批量模板列名映射：导入中心「阿里模板导入」与「阿里模板导出」都按这张表认列名。
-- 表头是数据不是代码——拿到真实模板后在后台改行即可，不用发版。默认行由插件启动时幂等补齐。
CREATE TABLE plg_website_ali_column (
  id BIGINT PRIMARY KEY,
  ali_header VARCHAR(100) NOT NULL,
  our_key VARCHAR(64) NOT NULL,
  aliases VARCHAR(500) NOT NULL DEFAULT '',
  required BOOLEAN NOT NULL DEFAULT FALSE,
  sort INT NOT NULL DEFAULT 0,
  note VARCHAR(200) NOT NULL DEFAULT '',
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_website_ali_column_header ON plg_website_ali_column (ali_header);
