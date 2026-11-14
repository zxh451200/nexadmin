-- 知识库插件：plg_kb_document（文档）、plg_kb_chunk（切块 + 向量）。只增不改；后续变更用 002_*.sql。
-- 向量以 JSON 浮点数组文本存储（无 pgvector 依赖，检索在应用内存里做余弦 topK）。
CREATE TABLE plg_kb_document (
  id BIGINT NOT NULL,
  title VARCHAR(200) NOT NULL,
  source_name VARCHAR(255) NOT NULL DEFAULT '',
  mime VARCHAR(128) NOT NULL DEFAULT '',
  size BIGINT NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL,
  chunk_count INT NOT NULL DEFAULT 0,
  model VARCHAR(128) NOT NULL DEFAULT '',
  dimension INT NOT NULL DEFAULT 0,
  error VARCHAR(500) NOT NULL DEFAULT '',
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id)
);
CREATE INDEX idx_plg_kb_doc_status ON plg_kb_document (status, updated_at);

CREATE TABLE plg_kb_chunk (
  id BIGINT NOT NULL,
  document_id BIGINT NOT NULL,
  seq INT NOT NULL,
  content TEXT NOT NULL,
  vector MEDIUMTEXT NOT NULL,
  created_at DATETIME(3) NOT NULL,
  updated_at DATETIME(3) NOT NULL,
  PRIMARY KEY (id),
  UNIQUE KEY uk_plg_kb_chunk_doc (document_id, seq)
);
