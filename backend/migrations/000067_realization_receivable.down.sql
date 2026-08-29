-- Rollback W-5. Kolom kebijakan dikembalikan dalam keadaan MATI untuk semua
-- tenant: menghidupkannya lagi adalah keputusan bisnis, bukan efek samping
-- rollback teknis. Baris audit yang sudah tertulis sengaja tidak dihapus —
-- tenant_policy_changes append-only.
ALTER TABLE tenants
    ADD COLUMN require_realization_settled TINYINT(1) NOT NULL DEFAULT 0;

DROP TABLE IF EXISTS charge_receivable_recognitions;

DROP INDEX idx_charge_items_recognized ON charge_items;

ALTER TABLE charge_items
    DROP COLUMN recognized_invoice_id,
    DROP COLUMN recognized_due_date,
    DROP COLUMN recognized_at,
    DROP COLUMN recognized_amount;
