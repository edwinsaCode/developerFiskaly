-- W-10 — Master Jenis Pengeluaran (Transaksi Pengeluaran).
--
-- MASALAH YANG DIPERBAIKI
-- Sebelum ini akun beban sebuah biaya ditentukan oleh enum TERTUTUP dua nilai
-- (domain.CostCategory: marketing → 5-3000, other → 5-4000). Padahal COA bawaan
-- sudah punya 5-4100 Gaji, 5-4200 Sewa Kantor, 5-4300 Utilitas, 5-4400
-- Perjalanan Dinas, 5-4500 Penyusutan — semuanya tidak bisa dicapai dari jalur
-- pencatatan biaya mana pun. Akibatnya listrik, internet, sewa, gaji, dan
-- transport semuanya menggumpal di 5-4000 Beban Umum & Administrasi.
--
-- BUKAN mesin biaya kedua: tabel transaksinya tetap `cost_entries`, jurnalnya
-- tetap lewat PostingService. Yang ditambahkan hanya MASTER pemetaan
-- jenis pengeluaran → akun beban, mengikuti pola W-1 realization_charge_types
-- (data-driven, fail-closed, mapping akun divalidasi sebelum disimpan, setiap
-- perubahan mapping tercatat di master_data_changes).
--
-- BD-2 (keputusan klien): PROJECT TAG ≠ REALISASI RAB. Realisasi RAB per item
-- hanya lahir dari budget_item_id; realisasi RAB per kategori dibaca dari akun
-- taksonomi CostCategory (1-3xxx, 5-3000, 5-4000). Karena itu pengeluaran
-- operasional yang akunnya termasuk himpunan taksonomi tersebut TIDAK BOLEH
-- di-tag ke proyek — ditegakkan fail-closed di service (INV-EXP-2), bukan di
-- schema, supaya pesan errornya bisa menjelaskan sebabnya.

-- ── 1. expense_types — master per tenant ────────────────────────────────────
CREATE TABLE expense_types (
    id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id            BIGINT UNSIGNED NOT NULL,
    code                 VARCHAR(50)     NOT NULL,
    name                 VARCHAR(200)    NOT NULL,
    -- Akun beban P&L tujuan debit. WAJIB bertipe expense di COA tenant —
    -- divalidasi di service sebelum baris ini pernah dipakai untuk menjurnal.
    expense_account_code VARCHAR(20)     NOT NULL,
    is_active            TINYINT(1)      NOT NULL DEFAULT 1,
    created_at           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    UNIQUE KEY uq_et_tenant_code (tenant_id, code),
    INDEX idx_et_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='W-10 master jenis pengeluaran operasional → akun beban (dikelola admin)';

-- ── 2. cost_entries.expense_type_id ─────────────────────────────────────────
-- NULL = jalur lama (biaya proyek ber-taksonomi CostCategory) — artinya TIDAK
-- berubah sedikit pun untuk seluruh baris historis. Terisi = pengeluaran
-- operasional yang akun debitnya berasal dari master, bukan dari enum.
ALTER TABLE cost_entries
    ADD COLUMN expense_type_id BIGINT UNSIGNED NULL AFTER category,
    ADD INDEX idx_ce_expense_type (tenant_id, expense_type_id);

-- ── 3. Seed default untuk seluruh tenant existing (idempoten) ───────────────
-- HANYA memetakan ke akun yang SUDAH ADA di COA bawaan (ledger/coa.go) dan
-- SUDAH bertipe expense. Tidak ada akun baru yang diciptakan migration ini:
-- menambah akun ke chart of accounts adalah keputusan pemilik, bukan efek
-- samping sebuah fitur.
--
-- Catatan: `atk` dan `software` memetakan ke 5-4000 karena COA bawaan belum
-- punya sub-akun sendiri untuk keduanya. Jenisnya tetap terpisah (untuk
-- pelaporan operasional per jenis), akun P&L-nya sama. Memecahnya menjadi baris
-- laba rugi sendiri membutuhkan akun COA baru — keputusan pemilik.
INSERT INTO expense_types (tenant_id, code, name, expense_account_code, is_active)
SELECT t.tenant_id, s.code, s.name, s.acc, 1
FROM (SELECT DISTINCT tenant_id FROM accounts) t
CROSS JOIN (
    SELECT 'gaji'        AS code, 'Gaji & Tunjangan'          AS name, '5-4100' AS acc UNION ALL
    SELECT 'sewa-kantor',        'Sewa Kantor',                        '5-4200' UNION ALL
    SELECT 'utilitas',           'Listrik, Air & Internet',            '5-4300' UNION ALL
    SELECT 'transport',          'Transport & Perjalanan Dinas',       '5-4400' UNION ALL
    SELECT 'penyusutan',         'Penyusutan',                         '5-4500' UNION ALL
    SELECT 'atk',                'ATK & Perlengkapan Kantor',          '5-4000' UNION ALL
    SELECT 'software',           'Software & Langganan',               '5-4000'
) s
WHERE EXISTS (
    SELECT 1 FROM accounts a
    WHERE a.tenant_id = t.tenant_id AND a.code = s.acc
)
AND NOT EXISTS (
    SELECT 1 FROM expense_types e
    WHERE e.tenant_id = t.tenant_id AND e.code = s.code
);
