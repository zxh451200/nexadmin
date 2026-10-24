CREATE TABLE IF NOT EXISTS plg_assistant_conversation (
    id CHAR(32) NOT NULL PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    slot SMALLINT NOT NULL,
    name VARCHAR(240) NOT NULL,
    messages TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at DATETIME(3) NOT NULL,
    updated_at DATETIME(3) NOT NULL,
    CONSTRAINT chk_assistant_slot CHECK (slot >= 0 AND slot < 30),
    UNIQUE KEY uq_assistant_owner_slot (tenant_id, user_id, slot),
    KEY idx_assistant_owner_updated (tenant_id, user_id, updated_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
