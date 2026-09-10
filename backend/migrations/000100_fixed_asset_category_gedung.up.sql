-- Item 4 (2026-09-05): tipe aset tetap baru "Gedung/Bangunan", selain
-- Kendaraan dan Peralatan Kantor. Pola identik 000086/000093 — clone atribut
-- dari akun aset tetap sejenis (1-4100 Kendaraan), lalu tambah baris master
-- fixed_asset_categories yang memetakannya ke akumulasi/beban penyusutan
-- BERSAMA (1-4900/5-4500) yang sudah dipakai kategori lain — tidak ada akun
-- baru selain akun aset itu sendiri.

-- 1) Akun 1-4200 untuk semua tenant existing (idempoten).
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '1-4200', 'Aset Tetap — Gedung/Bangunan', a.type, a.normal_balance, 0, '', 1, a.category
FROM accounts a
WHERE a.code = '1-4100'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '1-4200');

-- 2) Master kategori aset tetap "Gedung/Bangunan" untuk semua tenant existing
--    yang sudah punya master kategori aset tetap (idempoten; pola 000086).
INSERT INTO fixed_asset_categories
    (tenant_id, code, name, asset_account_code, accumulated_depreciation_account_code,
     depreciation_expense_account_code, default_useful_life_months, is_active)
SELECT t.tenant_id, 'gedung-bangunan', 'Gedung/Bangunan', '1-4200', '1-4900', '5-4500', 240, 1
FROM (SELECT DISTINCT tenant_id FROM fixed_asset_categories) t
WHERE EXISTS (
    SELECT 1 FROM accounts a WHERE a.tenant_id = t.tenant_id AND a.code = '1-4200'
) AND EXISTS (
    SELECT 1 FROM accounts a WHERE a.tenant_id = t.tenant_id AND a.code = '1-4900'
) AND EXISTS (
    SELECT 1 FROM accounts a WHERE a.tenant_id = t.tenant_id AND a.code = '5-4500'
) AND NOT EXISTS (
    SELECT 1 FROM fixed_asset_categories f WHERE f.tenant_id = t.tenant_id AND f.code = 'gedung-bangunan'
);
