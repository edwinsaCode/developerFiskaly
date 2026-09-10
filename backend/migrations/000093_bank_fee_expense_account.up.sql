-- UAT client fix (2026-09-03) — Rule #5 (Laba/Rugi): biaya provisi/administrasi
-- bank yang dipotong saat pencairan KPR, yang DITANGGUNG DEVELOPER (bukan
-- titipan customer — beda dari 2-2400 Titipan Realisasi, K-1 tetap berlaku
-- tanpa perubahan), sebelumnya tidak punya akun sendiri sehingga tidak pernah
-- diposting sebagai Beban P&L (silent gap dari nilai pencairan bersih vs
-- piutang bruto yang diselesaikan).
--
-- 1) Akun 5-3200 untuk semua tenant existing (idempoten; pola 000053 — clone
--    atribut dari akun beban sejenis 5-3100 Beban Komisi Penjualan).
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '5-3200', 'Beban Provisi & Administrasi Bank KPR', a.type, a.normal_balance, 0,
       'Provisi/biaya administrasi yang dipotong bank saat pencairan KPR — DITANGGUNG DEVELOPER (bukan titipan customer, beda dari 2-2400). Diakui saat pencairan diterima.',
       1, a.category
FROM accounts a
WHERE a.code = '5-3100'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '5-3200');
