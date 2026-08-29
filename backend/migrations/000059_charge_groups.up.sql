-- Billing Batch 2 — Charge Group (Titipan Realisasi & Addon Billing).
-- Business rule FINAL klien 2026-08-03 (K-1..K-5):
--   K-1 Seluruh biaya realisasi (PDAM/BPHTB/Notaris/Listrik) = TITIPAN (liability
--       2-2400). Bukan pendapatan, bukan bagian harga rumah.
--   K-2 Realisasi tidak menahan BAST secara default — kebijakan per tenant.
--   K-3 Alokasi pembayaran FLEXIBLE (keputusan admin, tanpa auto-FIFO/proporsional).
--   K-4 Sisa titipan (aktual < titipan) → refund / transfer, BUKAN pendapatan.
--   K-5 Aktual > titipan → outstanding tambahan otomatis.
--
-- Anti duplicate-SoT: kas masuk tetap SATU tabel (termin_payments), alokasi tetap
-- SATU sub-ledger (payment_allocations), aging tetap SATU mesin (BuildARAging).
-- Harga rumah TIDAK disentuh (tidak ada shadow group; primitif kanonik utuh).

-- ── 1. charge_groups — agregat penagihan di bawah SaleContract ───────────────
CREATE TABLE charge_groups (
    id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    sale_contract_id BIGINT UNSIGNED NOT NULL,   -- logical FK → sale_contracts.id
    unit_id          BIGINT UNSIGNED NOT NULL,   -- denormalisasi dari kontrak (query per unit)
    kind             VARCHAR(20)     NOT NULL,   -- 'realization' | 'addon'
    label            VARCHAR(120)    NOT NULL,
    status           VARCHAR(20)     NOT NULL DEFAULT 'open', -- open|settled|cancelled
    notes            VARCHAR(500)    NULL,
    created_by       BIGINT UNSIGNED NULL,
    created_at       DATETIME(3),
    updated_at       DATETIME(3),

    INDEX idx_cg_tenant   (tenant_id),
    INDEX idx_cg_contract (tenant_id, sale_contract_id),
    INDEX idx_cg_unit     (tenant_id, unit_id),
    CONSTRAINT chk_cg_kind   CHECK (kind IN ('realization','addon')),
    CONSTRAINT chk_cg_status CHECK (status IN ('open','settled','cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Billing Batch 2: kelompok tagihan (Titipan Realisasi / addon) per kontrak';

-- ── 2. charge_items — item tagihan di dalam grup ─────────────────────────────
-- amount = tagihan BERJALAN (bisa di-true-up K-4/K-5, SELALU ber-audit di
-- charge_item_adjustments). original_amount = tagihan awal, immutable.
CREATE TABLE charge_items (
    id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    charge_group_id  BIGINT UNSIGNED NOT NULL,   -- logical FK → charge_groups.id
    label            VARCHAR(120)    NOT NULL,   -- 'Notaris', 'PDAM', 'BPHTB', 'Listrik', ...
    amount           DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    original_amount  DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    due_date         DATE            NULL,       -- ikut AR aging bila diisi
    status           VARCHAR(20)     NOT NULL DEFAULT 'open', -- open|cancelled
    created_by       BIGINT UNSIGNED NULL,
    created_at       DATETIME(3),
    updated_at       DATETIME(3),

    INDEX idx_ci_tenant (tenant_id),
    INDEX idx_ci_group  (tenant_id, charge_group_id),
    CONSTRAINT chk_ci_status CHECK (status IN ('open','cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Billing Batch 2: item tagihan; amount berjalan di-true-up ber-audit';

-- ── 3. charge_item_adjustments — audit trail perubahan amount (append-only) ──
CREATE TABLE charge_item_adjustments (
    id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    charge_item_id   BIGINT UNSIGNED NOT NULL,
    old_amount       DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    new_amount       DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    reason           VARCHAR(30)     NOT NULL,   -- 'payout_overrun' (K-5) | 'trueup_settlement' (K-4)
    charge_payout_id BIGINT UNSIGNED NULL,       -- payout pemicu (K-5)
    created_by       BIGINT UNSIGNED NULL,
    created_at       DATETIME(3),
    updated_at       DATETIME(3),

    INDEX idx_cia_tenant (tenant_id),
    INDEX idx_cia_item   (tenant_id, charge_item_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Billing Batch 2: audit perubahan tagihan item (K-4/K-5)';

-- ── 4. charge_payouts — pembayaran ke vendor (Dr 2-2400 / Cr Kas) ────────────
CREATE TABLE charge_payouts (
    id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id         BIGINT UNSIGNED NOT NULL,
    charge_group_id   BIGINT UNSIGNED NOT NULL,
    charge_item_id    BIGINT UNSIGNED NOT NULL,
    amount            DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    date              DATETIME(3)     NOT NULL,
    bank_account_code VARCHAR(20)     NOT NULL,
    vendor            VARCHAR(120)    NOT NULL,
    notes             VARCHAR(500)    NULL,
    journal_entry_id  BIGINT UNSIGNED NOT NULL,  -- jurnal posted (immutable)
    idempotency_key   VARCHAR(64)     NULL,
    created_by        BIGINT UNSIGNED NULL,
    created_at        DATETIME(3),
    updated_at        DATETIME(3),

    INDEX idx_cp_tenant (tenant_id),
    INDEX idx_cp_group  (tenant_id, charge_group_id),
    INDEX idx_cp_item   (tenant_id, charge_item_id),
    UNIQUE KEY uk_cp_idem (tenant_id, idempotency_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Billing Batch 2: payout vendor dari titipan realisasi';

-- ── 5. charge_settlements — refund / transfer / void (K-4, append-only) ──────
CREATE TABLE charge_settlements (
    id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id         BIGINT UNSIGNED NOT NULL,
    charge_group_id   BIGINT UNSIGNED NOT NULL,   -- grup SUMBER dana
    action            VARCHAR(20)     NOT NULL,   -- refund|transfer_house|transfer_group|void_payment
    amount            DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    date              DATETIME(3)     NOT NULL,
    bank_account_code VARCHAR(20)     NULL,       -- refund: kas/bank tujuan
    target_group_id   BIGINT UNSIGNED NULL,       -- transfer_group: grup tujuan
    target_termin_id  BIGINT UNSIGNED NULL,       -- transfer_*: termin hasil di sisi tujuan
    voided_termin_id  BIGINT UNSIGNED NULL,       -- void_payment: termin yang dibatalkan
    journal_entry_id  BIGINT UNSIGNED NULL,       -- refund / transfer_group memo / jurnal pembalik void
    notes             VARCHAR(500)    NULL,
    idempotency_key   VARCHAR(64)     NULL,
    created_by        BIGINT UNSIGNED NULL,
    created_at        DATETIME(3),
    updated_at        DATETIME(3),

    INDEX idx_cs_tenant (tenant_id),
    INDEX idx_cs_group  (tenant_id, charge_group_id),
    UNIQUE KEY uk_cs_idem (tenant_id, idempotency_key),
    -- Satu void per termin — void ganda mustahil di level DB.
    UNIQUE KEY uk_cs_void (tenant_id, voided_termin_id),
    CONSTRAINT chk_cs_action CHECK (action IN ('refund','transfer_house','transfer_group','void_payment'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Billing Batch 2: disposisi sisa titipan + void pembayaran (K-4)';

-- ── 6. termin_payments: tautan ke grup (kas masuk tetap SATU tabel) ──────────
ALTER TABLE termin_payments
    ADD COLUMN charge_group_id BIGINT UNSIGNED NULL AFTER counts_toward_price,
    ADD INDEX idx_tp_charge_group (tenant_id, charge_group_id);

-- ── 7. payment_allocations: target charge_item (sub-ledger tetap SATU tabel) ─
-- allocation_type baru: 'charge_item' (alokasi manual admin, K-3) dan
-- 'charge_item_void' (mirror NEGATIF saat void — histori utuh, Invariant #5).
-- UNIQUE lama (tenant, termin, schedule_key) akan bentrok utk >1 item per
-- termin (schedule_key=0 semua) → diganti key yang memuat type + charge_item_key.
ALTER TABLE payment_allocations
    ADD COLUMN charge_item_id BIGINT UNSIGNED NULL AFTER payment_schedule_id,
    ADD COLUMN charge_item_key BIGINT UNSIGNED AS (COALESCE(charge_item_id, 0)) STORED,
    ADD INDEX idx_pa_charge_item (tenant_id, charge_item_id);

ALTER TABLE payment_allocations
    DROP INDEX uk_pa_termin_target,
    ADD UNIQUE KEY uk_pa_termin_target (tenant_id, termin_payment_id, allocation_type, schedule_key, charge_item_key);

-- ── 8. receipts & invoices: dokumen per grup ─────────────────────────────────
ALTER TABLE receipts
    ADD COLUMN charge_group_id BIGINT UNSIGNED NULL,
    ADD INDEX idx_receipts_charge_group (tenant_id, charge_group_id);

ALTER TABLE invoices
    ADD COLUMN charge_group_id BIGINT UNSIGNED NULL,
    ADD INDEX idx_invoices_charge_group (tenant_id, charge_group_id);

-- ── 9. Kebijakan BAST per tenant (K-2) — pola 000050 ─────────────────────────
-- 0 (default) = BAST TIDAK menunggu realisasi lunas; outstanding realisasi
-- tetap piutang operasional. 1 = BAST ditolak selama outstanding realisasi > 0.
ALTER TABLE tenants
    ADD COLUMN require_realization_settled TINYINT(1) NOT NULL DEFAULT 0;

-- ── 10. COA: 2-2400 Titipan Realisasi utk semua tenant existing ──────────────
-- (idempoten; clone atribut dari liability 2-2000 — pola 000053 §1.)
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '2-2400', 'Titipan Realisasi', a.type, a.normal_balance, 1,
       'Titipan biaya realisasi customer (PDAM/BPHTB/Notaris/Listrik) — K-1 2026-08-03: BUKAN pendapatan; terima Cr, payout vendor Dr. Sisa titipan wajib refund/transfer (K-4).',
       1, a.category
FROM accounts a
WHERE a.code = '2-2000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '2-2400');
