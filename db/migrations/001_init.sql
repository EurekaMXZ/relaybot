CREATE TABLE IF NOT EXISTS relays (
    id BIGSERIAL PRIMARY KEY,
    source_update_id BIGINT NOT NULL UNIQUE,
    code_value TEXT NOT NULL DEFAULT '',
    code_hash TEXT NOT NULL UNIQUE,
    code_hint TEXT NOT NULL,
    status TEXT NOT NULL,
    uploader_user_id BIGINT NOT NULL,
    uploader_chat_id BIGINT NOT NULL,
    source_message_id INTEGER NOT NULL,
    media_kind TEXT NOT NULL,
    telegram_file_id TEXT NOT NULL,
    telegram_file_unique_id TEXT NOT NULL,
    file_name TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    file_size_bytes BIGINT NOT NULL,
    caption TEXT NOT NULL DEFAULT '',
    delivery_count BIGINT NOT NULL DEFAULT 0,
    last_claimed_at TIMESTAMPTZ,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_relays_status_expires_at
    ON relays (status, expires_at);

CREATE INDEX IF NOT EXISTS idx_relays_uploader_user_id_created_at
    ON relays (uploader_user_id, created_at DESC);

CREATE TABLE IF NOT EXISTS relay_deliveries (
    id BIGSERIAL PRIMARY KEY,
    relay_id BIGINT NOT NULL REFERENCES relays(id) ON DELETE CASCADE,
    request_update_id BIGINT NOT NULL UNIQUE,
    claimer_user_id BIGINT NOT NULL,
    claimer_chat_id BIGINT NOT NULL,
    status TEXT NOT NULL,
    method TEXT NOT NULL DEFAULT '',
    telegram_out_message_id INTEGER,
    telegram_error_code TEXT NOT NULL DEFAULT '',
    telegram_error_desc TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_relay_deliveries_relay_id_created_at
    ON relay_deliveries (relay_id, created_at DESC);

CREATE INDEX IF NOT EXISTS idx_relay_deliveries_status_created_at
    ON relay_deliveries (status, created_at);
