-- 000105 — Kwitansi Piutang Proyek Lama (perluasan W-7, requirement #4/#5).
--
-- TIDAK ada mesin kwitansi kedua di sini: baris ini memperluas tabel
-- `receipts` yang sudah melayani KWT/KWB/KWR/KWD — kolom nullable baru + satu
-- unique key, persis pola charge_group_id (migration 000059), bukan tabel
-- terpisah yang tidak bisa disatukan dengan alur pembayaran yang sudah ada.
--
-- termin_payment_id dan unit_id menjadi NULLABLE karena piutang proyek lama
-- tidak pernah punya termin atau unit (tidak ada penjualan unit yang
-- menyertainya — hanya sisa tagihan dari transaksi sebelum sistem ini
-- dipakai). UNIQUE KEY uk_receipts_termin tetap aman: MySQL mengizinkan
-- banyak NULL pada unique key (NULL <> NULL secara SQL), jadi kwitansi legacy
-- AR yang semuanya NULL di kolom itu tidak pernah saling bentrok.
ALTER TABLE receipts
    MODIFY COLUMN termin_payment_id BIGINT UNSIGNED NULL,
    MODIFY COLUMN unit_id BIGINT UNSIGNED NULL,
    ADD COLUMN legacy_receivable_payment_id BIGINT UNSIGNED NULL,
    ADD UNIQUE KEY uk_receipts_legacy_payment (tenant_id, legacy_receivable_payment_id);

-- Jenis dokumen baru: KWL — kwitansi piutang proyek lama. Lewat SATU mesin
-- penomoran yang sudah ada (internal/document), bukan penomoran ad-hoc.
-- INSERT IGNORE mengikuti preseden 000070: tidak menimpa baris yang sudah ada
-- (mis. prefix yang diubah admin), dan aman dijalankan untuk semua tenant
-- sekaligus termasuk yang baru dibuat setelah migrasi ini (seed Go
-- internal/document/seed.go menghasilkan baris yang sama persis untuk tenant
-- berikutnya).
INSERT IGNORE INTO document_types
       (tenant_id, code, name, prefix, number_format, reset_policy, padding, is_active, is_system, created_at, updated_at)
SELECT t.id, 'legacy_ar', 'Kwitansi Piutang Proyek Lama', 'KWL', '{prefix}/{year}/{seq}', 'yearly', 6, TRUE, TRUE, NOW(3), NOW(3)
  FROM tenants t;
