-- Kembali ke definisi 000063 (live = belum batal, termasuk yang sudah lunas).
-- PERINGATAN: rollback GAGAL bila ada grup yang punya invoice lunas DAN invoice
-- baru — keadaan yang justru sah menurut 000064. Bersihkan dulu bila perlu.

ALTER TABLE invoices
    DROP INDEX uk_invoices_charge_group_live,
    DROP COLUMN charge_group_live;

ALTER TABLE invoices
    ADD COLUMN charge_group_live BIGINT UNSIGNED
        GENERATED ALWAYS AS (
            CASE WHEN invoice_type = 'REALISASI' AND status <> 'cancelled'
                 THEN charge_group_id END
        ) STORED,
    ADD UNIQUE KEY uk_invoices_charge_group_live (tenant_id, charge_group_live);
