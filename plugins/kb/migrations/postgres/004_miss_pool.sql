-- 004：未命中问题池。机器人检索不到时把访客问题攒起来，按热度排序，
-- 管理员补一条答案即可转成 QA 文档——知识库靠真实提问持续增长。只增不改。
CREATE TABLE plg_kb_miss (
  id BIGINT PRIMARY KEY,
  question VARCHAR(1000) NOT NULL,
  norm_hash VARCHAR(64) NOT NULL,
  ask_count INT NOT NULL DEFAULT 0,
  status VARCHAR(16) NOT NULL DEFAULT 'pending',
  document_id BIGINT NOT NULL DEFAULT 0,
  last_conv_no VARCHAR(64) NOT NULL DEFAULT '',
  first_at TIMESTAMP NOT NULL,
  last_at TIMESTAMP NOT NULL,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
CREATE UNIQUE INDEX uk_plg_kb_miss_norm ON plg_kb_miss (norm_hash);
CREATE INDEX idx_plg_kb_miss_status ON plg_kb_miss (status, ask_count);
