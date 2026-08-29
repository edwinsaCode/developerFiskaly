-- 000063 — Koreksi arsitektur (owner, 2026-08-06):
--
--   DICABUT : "satu Charge Group = satu akun titipan"
--   DIPASANG: "satu Charge Group = satu Invoice / Tagihan Biaya Realisasi"
--
-- Akun kewajiban titipan adalah ATRIBUT dari masing-masing RealizationChargeType.
-- Satu grup boleh memuat Notaris, BPHTB, PDAM, Listrik dengan akun kewajiban
-- BERBEDA sekaligus; yang pecah adalah BARIS JURNAL-nya, bukan grupnya. Dengan
-- begitu memberi akun berbeda pada tiap jenis biaya tidak menuntut perubahan
-- arsitektur apa pun.
--
-- Cara akunnya dibawa: SNAPSHOT di level item — pola yang sama dengan `label`.
-- `charge_type_code` tetap identitas/kunci akuntansi; `deposit_account_code`
-- adalah akun yang BERLAKU SAAT ITEM DIBUAT. Admin memindahkan sebuah jenis
-- biaya ke akun lain → item lama tetap membalik ke akun aslinya (append-only,
-- Invariant #5), item baru memakai akun baru. Tanpa snapshot ini jurnal pembalik
-- bisa memukul akun yang berbeda dari akun yang dulu dikredit.

-- ── 1. charge_items: snapshot akun titipan per item ─────────────────────────
ALTER TABLE charge_items
    ADD COLUMN deposit_account_code VARCHAR(20) NOT NULL DEFAULT '2-2400' AFTER charge_type_code,
    ADD INDEX idx_ci_deposit_account (tenant_id, deposit_account_code);

-- Backfill dari master. Item tanpa charge_type_code (histori pra-W-1) dan item
-- addon tetap memakai default 2-2400 — persis akun yang dulu dipakai, sehingga
-- residual dan jurnal pembalik histori tidak bergeser satu rupiah pun.
UPDATE charge_items ci
  JOIN realization_charge_types t
    ON t.tenant_id = ci.tenant_id
   AND t.code      = ci.charge_type_code
   SET ci.deposit_account_code = t.deposit_account_code;

-- ── 2. charge_settlement_lines: rincian per akun dari satu settlement ───────
-- Refund / transfer / void bergerak pada level GRUP dan tidak menunjuk item,
-- sehingga akun mana yang dilepaskan harus dicatat eksplisit — kalau tidak,
-- residual per akun mustahil direkonstruksi. Satu settlement = satu dokumen
-- (memo/refund) dengan N baris akun, cermin persis baris jurnalnya. Bentuk
-- induk-anak dipakai karena charge_settlements memegang kunci unik per dokumen
-- (idempotency, memo, void) — satu aksi tetap satu baris induk.
CREATE TABLE charge_settlement_lines (
    id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id            BIGINT UNSIGNED NOT NULL,
    charge_settlement_id BIGINT UNSIGNED NOT NULL,
    deposit_account_code VARCHAR(20)     NOT NULL,
    amount               DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    created_at           DATETIME(3),
    updated_at           DATETIME(3),

    INDEX idx_csl_tenant     (tenant_id),
    INDEX idx_csl_settlement (tenant_id, charge_settlement_id),
    UNIQUE KEY uk_csl_account (tenant_id, charge_settlement_id, deposit_account_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Rincian per akun kewajiban dari satu charge_settlement (append-only)';

-- Backfill: sebelum migrasi ini invariant lama menjamin satu grup hanya punya
-- SATU akun, jadi seluruh nominal settlement lama jatuh ke akun itu.
INSERT INTO charge_settlement_lines
       (tenant_id, charge_settlement_id, deposit_account_code, amount, created_at, updated_at)
SELECT cs.tenant_id,
       cs.id,
       COALESCE(x.acc, '2-2400'),
       cs.amount,
       cs.created_at,
       cs.updated_at
  FROM charge_settlements cs
  LEFT JOIN (
        SELECT tenant_id, charge_group_id, MIN(deposit_account_code) AS acc
          FROM charge_items
         GROUP BY tenant_id, charge_group_id
       ) x
    ON x.tenant_id       = cs.tenant_id
   AND x.charge_group_id = cs.charge_group_id;

-- ── 3. INV-CG-1: satu Charge Group = satu Invoice Realisasi ─────────────────
-- Ditegakkan di level DB, bukan sekadar pemindaian aplikasi. Invoice yang
-- dibatalkan melepas slotnya (dokumennya void — nomornya tetap dipegang, D-4),
-- sehingga grup bisa ditagih ulang setelah pembatalan yang ber-audit. Invoice
-- yang sudah LUNAS tidak melepas slot: satu grup tetap satu tagihan.
ALTER TABLE invoices
    ADD COLUMN charge_group_live BIGINT UNSIGNED
        GENERATED ALWAYS AS (
            CASE WHEN invoice_type = 'REALISASI' AND status <> 'cancelled'
                 THEN charge_group_id END
        ) STORED,
    ADD UNIQUE KEY uk_invoices_charge_group_live (tenant_id, charge_group_live);
