-- 000066 — W-3.1: tautan Jurnal ↔ Dokumen + 6 jenis dokumen kas.
--
-- Invariant yang disiapkan migrasi ini (INV-DOC-1, keputusan owner FINAL
-- 2026-08-07):
--
--   Setiap cash movement yang diposting harus memiliki TEPAT SATU Document
--   bernomor yang valid dan dapat ditelusuri DUA ARAH:
--       Document → Journal  dan  Journal → Document.
--
--   Yang dilarang:
--     * jurnal kas terposting tanpa dokumen;
--     * dokumen bernomor tanpa transaksi yang berhasil;
--     * satu cash movement menghasilkan dua dokumen;
--     * satu dokumen dipakai untuk dua cash movement yang seharusnya terpisah;
--     * nomor terpakai akibat transaksi gagal/rollback.
--
-- Penegakan fail-closed baru dipasang di W-3.5; migrasi ini menyiapkan
-- strukturnya saja. Kolom NULLABLE karena data lama belum tertaut (backfill
-- W-3.3) dan karena tidak semua jurnal menyentuh kas.

-- ── 1. Tautan Jurnal → Dokumen ──────────────────────────────────────────────
--
-- SATU kolom, bukan dua. Sempat dipertimbangkan menyimpan `journal_entry_id`
-- di `documents` juga agar "dua arah" terlihat eksplisit di kedua tabel, tapi
-- dua kolom yang menyimpan fakta yang sama bisa berbeda isi — dan saat berbeda,
-- tidak ada cara menentukan mana yang benar. Satu kolom + index memberi kedua
-- arah penelusuran dengan satu kebenaran:
--
--   Journal  → Document : journal_entries.document_id
--   Document → Journal  : index uk_je_document (lookup by document_id)
--
-- Logical FK (tanpa REFERENCES), konsisten dengan `documents.source_table/id`:
-- ledger tidak boleh punya ketergantungan skema ke modul dokumen.
ALTER TABLE journal_entries
    ADD COLUMN document_id BIGINT UNSIGNED NULL
        COMMENT 'Dokumen bernomor yang membuktikan jurnal ini (INV-DOC-1). NULL untuk jurnal non-kas & saldo awal.'
        AFTER source;

-- UNIQUE, bukan INDEX biasa: inilah yang menutup larangan "satu dokumen dipakai
-- untuk dua cash movement". MySQL mengizinkan NULL berulang pada unique index,
-- jadi jurnal non-kas tetap bebas.
ALTER TABLE journal_entries
    ADD UNIQUE KEY uk_je_document (tenant_id, document_id);

-- ── 2. Dokumen pembalik menunjuk dokumen aslinya ────────────────────────────
--
-- D-W3-5: jurnal pembalik mendapat dokumennya SENDIRI (jenis journal_reversal /
-- JR), bukan memakai ulang nomor dokumen asli. Kolom ini yang menjadikan
-- pembalikan bisa ditelusuri sebagai pasangan: JR/2026/000012 membalik
-- BKK/2026/000431. Tanpa ini, koreksi terlihat seperti dua pengeluaran terpisah.
ALTER TABLE documents
    ADD COLUMN reverses_document_id BIGINT UNSIGNED NULL
        COMMENT 'Dokumen yang dibalik oleh dokumen ini (khusus jenis pembalik). NULL untuk dokumen biasa.'
        AFTER source_id;

ALTER TABLE documents
    ADD INDEX idx_doc_reverses (tenant_id, reverses_document_id);

-- ── 3. Master 6 jenis dokumen kas ───────────────────────────────────────────
--
-- Katalog FINAL owner 2026-08-07, disusun menurut ECONOMIC OWNERSHIP — siapa
-- pemilik ekonomis uang yang bergerak — bukan menurut modul yang menerbitkan:
--
--   Cash In : KWT, KWB, KWR (sudah ada, 000065)
--             KWD  pencairan KPR — pembayarnya BANK, bukan customer
--             BKM  penerimaan kas perusahaan yang bukan pembayaran customer
--                  (setoran modal, penerimaan pinjaman, bunga bank, penerimaan
--                  lain, cash-in dari jurnal manual/berulang)
--
--   Cash Out: BKK  pengeluaran operasional/perusahaan — uang MILIK perusahaan
--             BTP  pembayaran titipan pihak ketiga — uang milik PIHAK KETIGA
--                  yang dititipkan (PDAM, listrik, BPHTB, notaris)
--             RFC  pengembalian dana ke customer — uang milik CUSTOMER
--             JR   pengeluaran/penerimaan akibat jurnal pembalik
--
-- Tiga seri kas keluar, bukan satu dan bukan delapan: satu seri membuat kas
-- perusahaan dan uang titipan tercampur dalam satu buku bukti — persis
-- pencampuran yang harus bisa dijelaskan terpisah ke pemilik dan ke auditor.
-- Delapan seri berarti nomor dokumen mengikuti modul, sehingga pemekaran modul
-- memaksa pemekaran seri.
--
-- is_system = TRUE: prefix/format/padding tetap bebas diubah admin, yang
-- dilarang hanya menonaktifkannya — resolver fail-closed akan menggagalkan
-- penerimaan/pengeluaran kas bila jenisnya mati.
-- INSERT IGNORE, bukan ON DUPLICATE KEY UPDATE: bila jenisnya sudah ada
-- (tenant yang lahir setelah seed Go dijalankan), konfigurasi milik admin
-- TIDAK boleh ditimpa migrasi. Diamkan baris yang sudah ada.
INSERT IGNORE INTO document_types
       (tenant_id, code, name, prefix, number_format, reset_policy, padding, is_active, is_system, created_at, updated_at)
SELECT t.id, d.code, d.name, d.prefix, '{prefix}/{year}/{seq}', 'yearly', 6, TRUE, TRUE, NOW(3), NOW(3)
  FROM tenants t
  CROSS JOIN (
      SELECT 'kpr_disbursement'   AS code, 'Kwitansi Pencairan KPR'        AS name, 'KWD' AS prefix
      UNION ALL SELECT 'cash_in',            'Bukti Kas Masuk',                  'BKM'
      UNION ALL SELECT 'cash_out',           'Bukti Kas Keluar',                 'BKK'
      UNION ALL SELECT 'third_party_payout', 'Bukti Pembayaran Titipan',         'BTP'
      UNION ALL SELECT 'customer_refund',    'Bukti Pengembalian Dana Customer', 'RFC'
      UNION ALL SELECT 'journal_reversal',   'Bukti Jurnal Pembalik',            'JR'
  ) d;
