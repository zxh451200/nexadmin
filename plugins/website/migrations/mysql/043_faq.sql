-- 常见问题板块：首页 FAQ section（enabled 关掉即整条隐藏，sort 控制顺序）。
CREATE TABLE plg_website_faq (
  id BIGINT NOT NULL PRIMARY KEY,
  question VARCHAR(300) NOT NULL,
  answer TEXT NOT NULL,
  sort INT NOT NULL DEFAULT 0,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  created_at DATETIME NOT NULL,
  updated_at DATETIME NOT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
