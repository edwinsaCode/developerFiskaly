-- 000070 — Pemulihan master jenis dokumen yang bolong.
--
-- MASALAH YANG DIPERBAIKI
-- Sebagian tenant hanya punya 6 dari 11 jenis dokumen inti: enam jenis kas
-- (KWD/BKM/BKK/BTP/RFC/JR) ada — itu disebar 000066 dengan INSERT IGNORE — tapi
-- lima jenis asli (KWT/KWB/KWR/MTI/INV) tidak. Seed di 000065 memakai INSERT
-- biasa, jadi begitu satu baris bentrok, sisa tenant dalam statement yang sama
-- tidak pernah kebagian.
--
-- AKIBATNYA DI LAPANGAN: resolver dokumen bersifat fail-closed (dan memang harus
-- begitu — nomor dokumen salah prefix jauh lebih sulit dibereskan daripada
-- transaksi yang gagal). Tenant yang bolong TIDAK BISA membuat booking, menerbitkan
-- kwitansi pembayaran rumah, kwitansi biaya realisasi, memo transfer internal,
-- maupun invoice: semuanya berhenti dengan error "jenis dokumen tidak dikenal".
--
-- YANG DILAKUKAN: menjamin kesebelas jenis inti ada untuk SETIAP tenant, persis
-- seperti seed Go (internal/document/seed.go) untuk tenant baru. INSERT IGNORE —
-- baris milik tenant yang sudah ada (termasuk prefix yang diubah admin) tidak
-- pernah ditimpa.
--
-- Ini master, bukan angka: tidak ada seri, saldo, atau nomor dokumen yang
-- dikarang di sini. Penomoran tetap mulai dari seri yang tersimpan di
-- document_sequences (kosong = mulai dari 1 pada tahun berjalan).

INSERT IGNORE INTO document_types
       (tenant_id, code, name, prefix, number_format, reset_policy, padding, is_active, is_system, created_at, updated_at)
SELECT t.id, d.code, d.name, d.prefix, '{prefix}/{year}/{seq}', 'yearly', 6, TRUE, TRUE, NOW(3), NOW(3)
  FROM tenants t
  CROSS JOIN (
      SELECT 'house_payment'     AS code, 'Kwitansi Pembayaran Rumah'  AS name, 'KWT' AS prefix
      UNION ALL SELECT 'booking',            'Kwitansi Booking',                 'KWB'
      UNION ALL SELECT 'realization',        'Kwitansi Biaya Realisasi',         'KWR'
      UNION ALL SELECT 'internal_transfer',  'Memo Transfer Internal',           'MTI'
      UNION ALL SELECT 'invoice',            'Invoice / Tagihan',                'INV'
      UNION ALL SELECT 'kpr_disbursement',   'Kwitansi Pencairan KPR',           'KWD'
      UNION ALL SELECT 'cash_in',            'Bukti Kas Masuk',                  'BKM'
      UNION ALL SELECT 'cash_out',           'Bukti Kas Keluar',                 'BKK'
      UNION ALL SELECT 'third_party_payout', 'Bukti Pembayaran Titipan',         'BTP'
      UNION ALL SELECT 'customer_refund',    'Bukti Pengembalian Dana Customer', 'RFC'
      UNION ALL SELECT 'journal_reversal',   'Bukti Jurnal Pembalik',            'JR'
  ) d;
