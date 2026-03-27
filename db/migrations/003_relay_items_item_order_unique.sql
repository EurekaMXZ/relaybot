WITH ordered_items AS (
    SELECT
        id,
        ROW_NUMBER() OVER (PARTITION BY relay_id ORDER BY item_order ASC, id ASC) AS new_item_order
    FROM relay_items
)
UPDATE relay_items AS items
SET item_order = ordered_items.new_item_order
FROM ordered_items
WHERE items.id = ordered_items.id
  AND items.item_order <> ordered_items.new_item_order;

DROP INDEX IF EXISTS idx_relay_items_relay_id_item_order;

CREATE UNIQUE INDEX IF NOT EXISTS uq_relay_items_relay_id_item_order
    ON relay_items (relay_id, item_order);
