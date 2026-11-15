-- 阿里批量模板列名映射：导入中心「阿里模板导入」与「阿里模板导出」都按这张表认列名。
-- 表头是数据不是代码——拿到真实模板后在后台改行即可，不用发版。默认行由插件启动时幂等补齐。
CREATE TABLE plg_website_ali_column (
  id BIGINT NOT NULL,
  ali_header VARCHAR(100) NOT NULL,
  our_key VARCHAR(64) NOT NULL,
  aliases VARCHAR(500) NOT NULL DEFAULT '',
  required TINYINT(1) NOT NULL DEFAULT 0,
  sort INT NOT NULL DEFAULT 0,
  note VARCHAR(200) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_website_ali_column_header (ali_header)
);
