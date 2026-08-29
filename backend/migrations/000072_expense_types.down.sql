-- Rollback W-10 — Master Jenis Pengeluaran.
-- Hanya membuang struktur yang dibuat 000072. Tidak ada jurnal, dokumen, atau
-- cost entry yang tersentuh: migration ini memang tidak pernah membuatnya.
ALTER TABLE cost_entries
    DROP INDEX idx_ce_expense_type,
    DROP COLUMN expense_type_id;

DROP TABLE IF EXISTS expense_types;
