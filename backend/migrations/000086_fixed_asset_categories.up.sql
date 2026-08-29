-- Fixed Asset — Master Kategori Aset Tetap.
--
-- COA bawaan sudah punya akun aset tetap (1-4000 Peralatan Kantor, 1-4100
-- Kendaraan), kontra-asetnya (1-4900 Akumulasi Penyusutan), dan akun bebannya
-- (5-4500 Beban Penyusutan) — tapi belum ada satu jalur pun yang pernah
-- memposting ke sana. Master ini memetakan satu kategori aset ke TIGA akun
-- sekaligus (aset, akumulasi penyusutan, beban penyusutan), mengikuti pola
-- W-10 expense_types / W-1 realization_charge_types: data-driven, fail-closed,
-- mapping akun divalidasi SEBELUM disimpan, setiap perubahan tercatat di
-- master_data_changes.
--
-- default_useful_life_months murni SARAN untuk UI (mengisi form), BUKAN
-- dipaksakan oleh service — dua aset di kategori yang sama boleh punya umur
-- ekonomis berbeda.

CREATE TABLE fixed_asset_categories (
    id                                     BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id                              BIGINT UNSIGNED NOT NULL,
    code                                   VARCHAR(50)     NOT NULL,
    name                                   VARCHAR(200)    NOT NULL,
    asset_account_code                     VARCHAR(20)     NOT NULL COMMENT 'Akun aset (Dr saat perolehan) — WAJIB tipe asset/debit',
    accumulated_depreciation_account_code  VARCHAR(20)     NOT NULL COMMENT 'Akun kontra-aset (Cr saat penyusutan) — WAJIB tipe asset/credit',
    depreciation_expense_account_code      VARCHAR(20)     NOT NULL COMMENT 'Akun beban (Dr saat penyusutan) — WAJIB tipe expense/debit',
    default_useful_life_months             INT UNSIGNED    NOT NULL DEFAULT 0 COMMENT 'Saran umur ekonomis untuk UI; tidak ditegakkan service',
    is_active                              TINYINT(1)      NOT NULL DEFAULT 1,
    created_at                             DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at                             DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    UNIQUE KEY uq_fac_tenant_code (tenant_id, code),
    INDEX idx_fac_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Fixed Asset — master kategori aset tetap → akun aset/akumulasi/beban (dikelola admin)';

-- Seed untuk seluruh tenant existing (idempoten). Hanya memetakan ke akun yang
-- SUDAH ADA di COA bawaan (ledger/coa.go); tidak ada akun baru dibuat di sini.
INSERT INTO fixed_asset_categories
    (tenant_id, code, name, asset_account_code, accumulated_depreciation_account_code,
     depreciation_expense_account_code, default_useful_life_months, is_active)
SELECT t.tenant_id, s.code, s.name, s.asset_acc, '1-4900', '5-4500', s.life, 1
FROM (SELECT DISTINCT tenant_id FROM accounts) t
CROSS JOIN (
    SELECT 'peralatan-kantor' AS code, 'Peralatan Kantor' AS name, '1-4000' AS asset_acc, 48 AS life UNION ALL
    SELECT 'kendaraan',              'Kendaraan',                   '1-4100',              96
) s
WHERE EXISTS (
    SELECT 1 FROM accounts a WHERE a.tenant_id = t.tenant_id AND a.code = s.asset_acc
) AND EXISTS (
    SELECT 1 FROM accounts a WHERE a.tenant_id = t.tenant_id AND a.code = '1-4900'
) AND EXISTS (
    SELECT 1 FROM accounts a WHERE a.tenant_id = t.tenant_id AND a.code = '5-4500'
) AND NOT EXISTS (
    SELECT 1 FROM fixed_asset_categories f WHERE f.tenant_id = t.tenant_id AND f.code = s.code
);
