-- 000071 — Pemulihan seri penomoran dokumen yang tertinggal.
--
-- MASALAH YANG DIPERBAIKI
-- Pengambilalihan seri di 000065 §5 (receipt_sequences/invoice_sequences →
-- document_sequences) tidak sampai ke sebagian tenant — gejalanya sama dengan
-- 000070: satu statement, sekali gagal, sisa tenant tidak kebagian. Akibatnya
-- engine baru mulai menghitung dari 1 padahal tenant itu SUDAH pernah menerbitkan
-- KWT/KWB/KWR/INV nomor 1. Hasilnya bukan nomor kembar — untung ada unique key —
-- melainkan seluruh penerbitan kwitansi, booking, dan invoice tenant tersebut
-- berhenti dengan Duplicate entry.
--
-- PRINSIPNYA SAMA DENGAN 000065: forward-only. Seri disamakan dengan nomor
-- TERTINGGI yang sudah benar-benar terbit per (tenant, jenis, tahun) — jadi
-- nomor berikutnya pasti belum terpakai. Tidak ada nomor lama yang diubah,
-- tidak ada dokumen yang dikarang. Lebih baik melompati nomor daripada
-- menerbitkan nomor kembar.
--
-- Tahun diambil dari NOMORNYA (bukan YEAR(NOW())), karena seri disimpan per
-- tahun fiskal: dokumen 2025 tidak boleh menggeser seri 2026.

-- ── 1. Dari dokumen yang sudah terbit: kwitansi ─────────────────────────────
INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT tenant_id, doc_type, fy, MAX(seq), NOW(3), NOW(3)
  FROM (
      SELECT tenant_id,
             receipt_type AS doc_type,
             CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(receipt_number, '/', 2), '/', -1) AS UNSIGNED) AS fy,
             CAST(SUBSTRING_INDEX(receipt_number, '/', -1) AS UNSIGNED) AS seq
        FROM receipts
       WHERE receipt_number REGEXP '^[A-Z]+/[0-9]{4}/[0-9]+$'
  ) x
 WHERE fy BETWEEN 2000 AND 2100
 GROUP BY tenant_id, doc_type, fy
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));

-- ── 2. Dari dokumen yang sudah terbit: invoice ──────────────────────────────
INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT tenant_id, 'invoice', fy, MAX(seq), NOW(3), NOW(3)
  FROM (
      SELECT tenant_id,
             CAST(SUBSTRING_INDEX(SUBSTRING_INDEX(invoice_number, '/', 2), '/', -1) AS UNSIGNED) AS fy,
             CAST(SUBSTRING_INDEX(invoice_number, '/', -1) AS UNSIGNED) AS seq
        FROM invoices
       WHERE invoice_number REGEXP '^[A-Z]+/[0-9]{4}/[0-9]+$'
  ) y
 WHERE fy BETWEEN 2000 AND 2100
 GROUP BY tenant_id, fy
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));

-- ── 3. Dari registry dokumen (engine baru) ──────────────────────────────────
-- Menjaga seri tidak pernah turun di bawah dokumen yang sudah tercatat di
-- registry, termasuk jenis yang tidak punya tabel sendiri (BKM/BKK/BTP/RFC/JR).
INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT tenant_id, document_type_code, fiscal_year, MAX(sequence_no), NOW(3), NOW(3)
  FROM documents
 GROUP BY tenant_id, document_type_code, fiscal_year
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));

-- ── 4. Dari counter engine lama yang belum diambil alih ─────────────────────
-- next_val di kedua tabel lama menyimpan nomor TERAKHIR yang terpakai (lihat
-- catatan 000065 §5), jadi pemetaannya lurus ke last_val. Dipakai pada tahun
-- berjalan: counter lama bersifat perpetual, dan yang perlu dilindungi adalah
-- tabrakan di tahun yang sedang berjalan.
INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT rs.tenant_id, rs.doc_type, YEAR(NOW()), rs.next_val, NOW(3), NOW(3)
  FROM receipt_sequences rs
 WHERE rs.next_val > 0
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));

INSERT INTO document_sequences (tenant_id, document_type_code, fiscal_year, last_val, created_at, updated_at)
SELECT is2.tenant_id, 'invoice', YEAR(NOW()), is2.next_val, NOW(3), NOW(3)
  FROM invoice_sequences is2
 WHERE is2.next_val > 0
ON DUPLICATE KEY UPDATE last_val = GREATEST(document_sequences.last_val, VALUES(last_val));
