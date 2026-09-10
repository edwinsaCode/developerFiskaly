-- Catatan: rollback ini gagal bila sudah ada baris dengan payment_schedule_id
-- NULL (konsumsi otomatis Akad/pembatalan) — hapus/relokasi baris tsb dahulu.
ALTER TABLE credit_applications
    MODIFY COLUMN payment_schedule_id BIGINT UNSIGNED NOT NULL;
