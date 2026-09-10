-- Catatan: rollback ini gagal bila sudah ada baris dengan sale_contract_id
-- NULL (konsumsi otomatis tanpa kontrak formal) — hapus/relokasi baris tsb dahulu.
ALTER TABLE credit_applications
    MODIFY COLUMN sale_contract_id BIGINT UNSIGNED NOT NULL;
