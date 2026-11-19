-- 常见问题板块（与 mysql/043 同构）。
CREATE TABLE plg_website_faq (
  id BIGINT PRIMARY KEY,
  question VARCHAR(300) NOT NULL DEFAULT '',
  answer TEXT NOT NULL,
  sort INT NOT NULL DEFAULT 0,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL,
  updated_at TIMESTAMP NOT NULL
);
