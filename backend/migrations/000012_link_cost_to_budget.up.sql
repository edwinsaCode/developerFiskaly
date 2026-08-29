-- Phase 5 RAB link: tambah budget_item_id ke cost_entries.
-- Nullable = cost entry boleh tidak tertaut ke RAB (backward compatible).
-- ON DELETE SET NULL: jika budget item dihapus, referensi di cost entry dibuat NULL
-- (bukan error; cost entry dan jurnalnya tetap valid — hanya kehilangan tautan RAB).

ALTER TABLE cost_entries
    ADD COLUMN budget_item_id BIGINT UNSIGNED NULL
        COMMENT 'Tautan opsional ke budget_items. NULL = tidak tertaut RAB.',
    ADD INDEX idx_cost_entries_budget_item (budget_item_id),
    ADD CONSTRAINT fk_cost_entries_budget_item
        FOREIGN KEY (budget_item_id) REFERENCES budget_items(id)
        ON DELETE SET NULL;
