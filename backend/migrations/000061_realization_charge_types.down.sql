-- Rollback W-1 — Master Jenis Biaya Realisasi.
-- Hanya membuang struktur yang dibuat 000061. Tidak ada jurnal, alokasi, atau
-- pembayaran yang tersentuh: migration ini memang tidak pernah membuatnya.
ALTER TABLE charge_items
    DROP INDEX idx_ci_charge_type,
    DROP COLUMN charge_type_code;

DROP TABLE IF EXISTS master_data_changes;
DROP TABLE IF EXISTS realization_charge_types;
