CREATE TABLE IF NOT EXISTS plg_assistant_conversation (
    id CHAR(32) PRIMARY KEY,
    tenant_id BIGINT NOT NULL,
    user_id BIGINT NOT NULL,
    slot SMALLINT NOT NULL CHECK (slot >= 0 AND slot < 30),
    name VARCHAR(240) NOT NULL,
    messages TEXT NOT NULL,
    revision BIGINT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL,
    UNIQUE (tenant_id, user_id, slot)
);
CREATE INDEX IF NOT EXISTS idx_assistant_owner_updated
    ON plg_assistant_conversation (tenant_id, user_id, updated_at);
