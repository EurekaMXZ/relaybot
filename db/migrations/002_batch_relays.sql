CREATE TABLE IF NOT EXISTS relay_items (
    id BIGSERIAL PRIMARY KEY,
    relay_id BIGINT NOT NULL REFERENCES relays(id) ON DELETE CASCADE,
    source_update_id BIGINT NOT NULL UNIQUE,
    source_message_id INTEGER NOT NULL,
    media_group_id TEXT NOT NULL DEFAULT '',
    item_order INTEGER NOT NULL,
    media_kind TEXT NOT NULL,
    telegram_file_id TEXT NOT NULL,
    telegram_file_unique_id TEXT NOT NULL,
    file_name TEXT NOT NULL DEFAULT '',
    mime_type TEXT NOT NULL DEFAULT '',
    file_size_bytes BIGINT NOT NULL,
    caption TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_relay_items_relay_id_item_order
    ON relay_items (relay_id, item_order);

ALTER TABLE relays ALTER COLUMN source_update_id DROP NOT NULL;
ALTER TABLE relays ALTER COLUMN code_hash DROP NOT NULL;
ALTER TABLE relays ALTER COLUMN source_message_id DROP NOT NULL;
ALTER TABLE relays ALTER COLUMN media_kind DROP NOT NULL;
ALTER TABLE relays ALTER COLUMN telegram_file_id DROP NOT NULL;
ALTER TABLE relays ALTER COLUMN telegram_file_unique_id DROP NOT NULL;
ALTER TABLE relays ALTER COLUMN file_size_bytes DROP NOT NULL;
ALTER TABLE relays ALTER COLUMN code_hint SET DEFAULT '';

INSERT INTO relay_items (
    relay_id,
    source_update_id,
    source_message_id,
    media_group_id,
    item_order,
    media_kind,
    telegram_file_id,
    telegram_file_unique_id,
    file_name,
    mime_type,
    file_size_bytes,
    caption,
    created_at
)
SELECT
    r.id,
    r.source_update_id,
    r.source_message_id,
    '',
    1,
    r.media_kind,
    r.telegram_file_id,
    r.telegram_file_unique_id,
    r.file_name,
    r.mime_type,
    r.file_size_bytes,
    r.caption,
    r.created_at
FROM relays r
WHERE r.source_update_id IS NOT NULL
ON CONFLICT (source_update_id) DO NOTHING;

CREATE INDEX IF NOT EXISTS idx_relays_status_updated_at
    ON relays (status, updated_at);
