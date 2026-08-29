-- 000065 — W-2 Document Domain.
--
-- Keputusan owner FINAL 2026-08-07:
--   * SATU mesin penomoran dokumen. `receipt_sequences` dan `invoice_sequences`
--     tidak lagi dipakai — digantikan `document_sequences` dengan kunci
--     (tenant_id, document_type_code, fiscal_year).
--   * Reset tahunan berlaku FORWARD-ONLY, sejak engine baru dipakai.
--   * Dokumen historis TIDAK berubah sedikit pun. Ini perubahan kebijakan,
--     bukan migrasi histori.
--   * Tahun baru mulai dari 000001.
--   * Yang berbeda antar jenis dokumen hanyalah KONFIGURASI (prefix, format,
--     kebijakan reset) — bukan enginenya.
--
-- Titik paling rawan di migrasi ini: seri tahun BERJALAN tidak boleh mulai dari
-- 1. Kalau tenant sudah menerbitkan KWT/2026/000042, engine baru yang memulai
-- 2026 dari 1 akan menabrak nomor yang sudah dicetak dan diserahkan ke customer.
-- Karena itu tahun berjalan DI-SEED dari counter lama, dan hanya tahun-tahun
-- SESUDAHNYA yang mulai dari 1.

-- ── 1. Master jenis dokumen ─────────────────────────────────────────────────
-- Perilaku penomoran adalah DATA, bukan cabang if di kode. Menambah jenis
-- dokumen baru (Cash Voucher, Bukti Kas Keluar, …) cukup satu baris di sini.
CREATE TABLE document_types (
    id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id     BIGINT UNSIGNED NOT NULL,
    code          VARCHAR(32)     NOT NULL,
    name          VARCHAR(100)    NOT NULL,
    prefix        VARCHAR(10)     NOT NULL,
    -- Format memakai placeholder: {prefix} {year} {month} {seq}.
    number_format VARCHAR(60)     NOT NULL DEFAULT '{prefix}/{year}/{seq}',
    -- 'yearly' → seri kembali ke 1 tiap ganti tahun fiskal.
    -- 'never'  → seri berjalan terus (fiscal_year disimpan 0).
    reset_policy  VARCHAR(16)     NOT NULL DEFAULT 'yearly',
    padding       TINYINT UNSIGNED NOT NULL DEFAULT 6,
    is_active     BOOLEAN         NOT NULL DEFAULT TRUE,
    -- is_system: jenis dokumen yang dipakai alur inti (kwitansi, invoice, memo).
    -- Prefix/format/padding-nya tetap bebas diubah admin — yang dilarang hanya
    -- MENONAKTIFKANNYA, karena resolver fail-closed akan menggagalkan penerimaan
    -- pembayaran. Flag ini DATA, bukan daftar kode di dalam kode program.
    is_system     BOOLEAN         NOT NULL DEFAULT FALSE,
    created_at    DATETIME(3),
    updated_at    DATETIME(3),
    INDEX idx_dt_tenant (tenant_id),
    UNIQUE KEY uk_dt_code (tenant_id, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Master jenis dokumen: prefix, format, kebijakan reset (W-2)';

-- ── 2. SSOT penomoran ───────────────────────────────────────────────────────
-- Satu tabel untuk SELURUH jenis dokumen. fiscal_year = 0 untuk jenis dokumen
-- yang tidak pernah reset, supaya bentuk kuncinya tetap seragam.
CREATE TABLE document_sequences (
    tenant_id          BIGINT UNSIGNED NOT NULL,
    document_type_code VARCHAR(32)     NOT NULL,
    fiscal_year        SMALLINT UNSIGNED NOT NULL,
    -- SEMANTIK: nomor TERAKHIR yang sudah dipakai, bukan nomor berikutnya.
    -- Dinamai `last_val` (bukan `next_val` seperti tabel lama) justru karena
    -- tabel lama menyimpan makna ini di bawah nama yang menyesatkan; salah baca
    -- satu kali saja berarti dua dokumen bernomor sama.
    last_val           BIGINT UNSIGNED NOT NULL DEFAULT 0,
    created_at         DATETIME(3),
    updated_at         DATETIME(3),
    PRIMARY KEY (tenant_id, document_type_code, fiscal_year)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='SSOT penomoran dokumen — satu engine untuk semua jenis (W-2)';

-- ── 3. Registry dokumen ─────────────────────────────────────────────────────
-- Setiap nomor yang pernah diterbitkan tercatat di sini, apa pun jenisnya.
-- Append-only: nomor dokumen tidak pernah diubah atau dipakai ulang.
CREATE TABLE documents (
    id                 BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id          BIGINT UNSIGNED NOT NULL,
    document_type_code VARCHAR(32)     NOT NULL,
    number             VARCHAR(40)     NOT NULL,
    fiscal_year        SMALLINT UNSIGNED NOT NULL,
    sequence_no        BIGINT UNSIGNED NOT NULL,
    issued_at          DATETIME(3)     NOT NULL,
    -- Asal dokumen: tabel + id baris yang menerbitkannya (receipts, invoices,
    -- charge_settlements, …). Logical FK — tidak ada constraint lintas modul.
    source_table       VARCHAR(40)     NOT NULL,
    source_id          BIGINT UNSIGNED NOT NULL,
    amount             DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    created_by         BIGINT UNSIGNED NULL,
    created_at         DATETIME(3),
    updated_at         DATETIME(3),
    INDEX idx_doc_tenant   (tenant_id),
    -- sequence_no ikut di index supaya MAX(sequence_no) per (tipe, tahun) —
    -- dipakai engine untuk menyembuhkan seri tahun baru — jadi index lookup.
    INDEX idx_doc_type     (tenant_id, document_type_code, fiscal_year, sequence_no),
    INDEX idx_doc_issued   (tenant_id, issued_at),
    UNIQUE KEY uk_doc_number (tenant_id, document_type_code, number),
    UNIQUE KEY uk_doc_source (tenant_id, source_table, source_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Registry seluruh dokumen bernomor (append-only, W-2)';

-- ── 4. Seed master untuk tenant yang SUDAH ada ──────────────────────────────
-- Kode jenis dokumen sengaja SAMA dengan doc_type lama di receipt_sequences,
-- supaya pemetaan seri lama → baru satu-lawan-satu dan tidak perlu tebakan.
INSERT INTO document_types
       (tenant_id, code, name, prefix, number_format, reset_policy, padding, is_active, is_system, created_at, updated_at)
SELECT t.id, d.code, d.name, d.prefix, '{prefix}/{year}/{seq}', 'yearly', 6, TRUE, TRUE, NOW(3), NOW(3)
  FROM tenants t
  CROSS JOIN (
      SELECT 'house_payment'     AS code, 'Kwitansi Pembayaran Rumah'  AS name, 'KWT' AS prefix
      UNION ALL SELECT 'booking',           'Kwitansi Booking',              'KWB'
      UNION ALL SELECT 'realization',       'Kwitansi Biaya Realisasi',      'KWR'
      UNION ALL SELECT 'internal_transfer', 'Memo Transfer Internal',        'MTI'
      UNION ALL SELECT 'invoice',           'Invoice / Tagihan',             'INV'
  ) d;

-- ── 5. Ambil alih seri berjalan dari kedua engine lama ──────────────────────
-- Inti keputusan forward-only. Tahun berjalan melanjutkan counter lama; tahun
-- berikutnya baru mulai dari 1 (itu perilaku alami engine, tanpa baris di sini).
--
-- YEAR(NOW()) dipakai — bukan tahun dokumen terakhir — karena counter lama
-- memang perpetual: nilainya adalah "berapa nomor yang sudah terpakai", dan
-- yang perlu dilindungi adalah tabrakan pada tahun yang sedang berjalan.

-- Catatan penting: `next_val` di KEDUA tabel lama menyimpan nomor TERAKHIR yang
-- terpakai (pola `ON DUPLICATE KEY UPDATE next_val = next_val + 1` lalu SELECT).
-- Jadi pemetaannya lurus ke `last_val`, tanpa +1 dan tanpa -1.

-- 5a. Dari receipt_sequences (house_payment, booking, realization, internal_transfer).
INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT rs.tenant_id, rs.doc_type, YEAR(NOW()), rs.next_val, NOW(3), NOW(3)
  FROM receipt_sequences rs
 WHERE rs.next_val > 0
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));

-- 5b. Dari invoice_sequences (satu seri per tenant, tanpa doc_type).
INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT is2.tenant_id, 'invoice', YEAR(NOW()), is2.next_val, NOW(3), NOW(3)
  FROM invoice_sequences is2
 WHERE is2.next_val > 0
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));

-- 5c. Jaring pengaman: bila sebuah tenant punya dokumen dengan nomor urut LEBIH
-- TINGGI daripada counter (mis. counter pernah di-reset manual), seri berjalan
-- ikut naik. Lebih baik melompati nomor daripada menerbitkan nomor kembar.
INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT tenant_id, doc_type, YEAR(NOW()), MAX(seq), NOW(3), NOW(3)
  FROM (
      SELECT tenant_id, receipt_type AS doc_type,
             CAST(SUBSTRING_INDEX(receipt_number, '/', -1) AS UNSIGNED) AS seq
        FROM receipts
       WHERE receipt_number LIKE CONCAT('%/', YEAR(NOW()), '/%')
      UNION ALL
      SELECT tenant_id, 'invoice',
             CAST(SUBSTRING_INDEX(invoice_number, '/', -1) AS UNSIGNED)
        FROM invoices
       WHERE invoice_number LIKE CONCAT('%/', YEAR(NOW()), '/%')
  ) x
 GROUP BY tenant_id, doc_type
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));

-- ── 6. Backfill registry dari dokumen yang sudah terbit ─────────────────────
-- Registry harus lengkap sejak hari pertama, kalau tidak "dokumen tanpa jejak"
-- akan terlihat seperti temuan audit padahal hanya artefak migrasi.
-- fiscal_year & sequence_no dibaca dari nomor itu sendiri — sumbernya nomor
-- yang sudah dicetak, bukan tebakan.

INSERT IGNORE INTO documents
       (tenant_id, document_type_code, number, fiscal_year, sequence_no, issued_at,
        source_table, source_id, amount, created_by, created_at, updated_at)
SELECT r.tenant_id, r.receipt_type, r.receipt_number,
       CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(r.receipt_number, '/', 2), '/', -1) AS UNSIGNED),
       CAST(SUBSTRING_INDEX(r.receipt_number, '/', -1) AS UNSIGNED),
       r.received_at, 'receipts', r.id, r.amount, r.created_by, NOW(3), NOW(3)
  FROM receipts r;

INSERT IGNORE INTO documents
       (tenant_id, document_type_code, number, fiscal_year, sequence_no, issued_at,
        source_table, source_id, amount, created_by, created_at, updated_at)
SELECT i.tenant_id, 'invoice', i.invoice_number,
       CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(i.invoice_number, '/', 2), '/', -1) AS UNSIGNED),
       CAST(SUBSTRING_INDEX(i.invoice_number, '/', -1) AS UNSIGNED),
       i.issue_date, 'invoices', i.id, i.amount, i.created_by, NOW(3), NOW(3)
  FROM invoices i;

INSERT IGNORE INTO documents
       (tenant_id, document_type_code, number, fiscal_year, sequence_no, issued_at,
        source_table, source_id, amount, created_by, created_at, updated_at)
SELECT cs.tenant_id, 'internal_transfer', cs.memo_number,
       CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(cs.memo_number, '/', 2), '/', -1) AS UNSIGNED),
       CAST(SUBSTRING_INDEX(cs.memo_number, '/', -1) AS UNSIGNED),
       cs.date, 'charge_settlements', cs.id, cs.amount, cs.created_by, NOW(3), NOW(3)
  FROM charge_settlements cs
 WHERE cs.memo_number IS NOT NULL AND cs.memo_number <> '';
