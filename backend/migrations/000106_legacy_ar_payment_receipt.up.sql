-- 000106 — Snapshot kwitansi (KWL) di baris pembayaran piutang proyek lama.
--
-- Requirement UX: pengguna tidak boleh pindah ke Buku Dokumen hanya untuk
-- mencetak kwitansi yang baru saja terbit dari pelunasan piutang lama.
-- Halaman detail piutang perlu menampilkan tombol cetak langsung di baris
-- "Riwayat Pembayaran" — itu perlu nomor kwitansi tanpa join ke tabel
-- receipts, mengikuti pola document_id/document_number yang sudah ada di
-- tabel ini (snapshot supaya riwayat terbaca tanpa join).
ALTER TABLE legacy_receivable_payments
    ADD COLUMN receipt_id BIGINT UNSIGNED NULL,
    ADD COLUMN receipt_number VARCHAR(40) NULL;
