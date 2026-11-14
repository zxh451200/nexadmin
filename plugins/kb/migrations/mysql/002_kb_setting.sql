-- 知识库插件设置表（检索阈值 / TopK / 提示模板，plg_kb_setting 单行 JSON）：001 漏建，老库与新库统一从这里补。只增不改。
CREATE TABLE plg_kb_setting (
  id BIGINT NOT NULL,
  k VARCHAR(64) NOT NULL,
  v TEXT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_kb_setting_k (k)
);
