-- Increment 9 — Commission Engine (blueprint §3: Rule → Commission → Approval →
-- Payment → Journal, + clawback bila sale batal §1).
--
-- Ledger (semua saldo derived):
--   payable : Dr Beban Komisi 5-3100 / Cr Utang Komisi 2-6200 (akrual)
--   paid    : Dr Utang Komisi 2-6200 / Cr Bank
--   cancel (payable, belum dibayar): reversing akrual
--   clawback (sudah dibayar, sale batal): Dr Piutang Lain-lain 1-2100
--                                         / Cr Beban Komisi 5-3100 (recovery)
-- Owner: Tenant (beban perusahaan); references Sale Record, Sales Person, Project.

-- 1) Commission Rule (config; snapshot provenance disalin ke entri komisi).
CREATE TABLE IF NOT EXISTS commission_rules (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id       BIGINT UNSIGNED NOT NULL,
    name            VARCHAR(200)    NOT NULL,
    -- basis: percent_of_sale | flat_per_unit diimplementasikan; tiered =
    -- vocabulary SEAM (engine menolak sampai formula terdaftar — pola TaxFormula).
    basis           VARCHAR(20)     NOT NULL,
    rate            DECIMAL(10,6)   NOT NULL DEFAULT '0.000000' COMMENT 'fraksi (0.025 = 2.5%) utk percent_of_sale',
    flat_amount     DECIMAL(20,4)   NOT NULL DEFAULT '0.0000'   COMMENT 'utk flat_per_unit',
    -- trigger: at_bast diimplementasikan; at_collection|at_lunas = SEAM.
    trigger_event   VARCHAR(20)     NOT NULL DEFAULT 'at_bast',
    -- Scope (NULL = berlaku semua): per salesperson / proyek / tipe unit.
    sales_person_id BIGINT UNSIGNED NULL,
    project_id      BIGINT UNSIGNED NULL,
    unit_type       VARCHAR(50)     NULL,
    expense_account VARCHAR(20)     NOT NULL DEFAULT '5-3100',
    payable_account VARCHAR(20)     NOT NULL DEFAULT '2-6200',
    effective_from  DATE            NOT NULL,
    effective_to    DATE            NULL,
    is_active       TINYINT(1)      NOT NULL DEFAULT 1,
    created_by      BIGINT UNSIGNED NULL,
    created_at      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at      DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_cr_tenant (tenant_id),
    KEY idx_cr_active (tenant_id, is_active),
    CONSTRAINT chk_cr_basis   CHECK (basis IN ('percent_of_sale','flat_per_unit','tiered')),
    CONSTRAINT chk_cr_trigger CHECK (trigger_event IN ('at_bast','at_collection','at_lunas'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 2) Commission (entri per sale × rule; snapshot rate — append-only audit).
CREATE TABLE IF NOT EXISTS commissions (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id       BIGINT UNSIGNED NOT NULL,
    rule_id         BIGINT UNSIGNED NOT NULL,
    sale_record_id  BIGINT UNSIGNED NOT NULL,
    sale_contract_id BIGINT UNSIGNED NULL,
    project_id      BIGINT UNSIGNED NOT NULL,
    unit_id         BIGINT UNSIGNED NOT NULL,
    sales_person_id BIGINT UNSIGNED NOT NULL,
    basis_amount    DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'dasar (harga jual DPP)',
    rate_snapshot   DECIMAL(10,6)   NOT NULL DEFAULT '0.000000',
    amount          DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    status          VARCHAR(20)     NOT NULL DEFAULT 'calculated',

    accrual_journal_id  BIGINT UNSIGNED NULL,
    payment_journal_id  BIGINT UNSIGNED NULL,
    clawback_journal_id BIGINT UNSIGNED NULL,
    bank_account_code   VARCHAR(20)     NOT NULL DEFAULT '',

    -- Audit actor per transisi (pola IMPL-5).
    calculated_at DATETIME(3)     NULL,
    approved_at   DATETIME(3)     NULL,
    approved_by   BIGINT UNSIGNED NULL,
    payable_at    DATETIME(3)     NULL,
    payable_by    BIGINT UNSIGNED NULL,
    paid_at       DATETIME(3)     NULL,
    paid_by       BIGINT UNSIGNED NULL,
    cancelled_at  DATETIME(3)     NULL,
    cancelled_by  BIGINT UNSIGNED NULL,
    cancel_reason VARCHAR(500)    NOT NULL DEFAULT '',

    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_cm_tenant  (tenant_id),
    KEY idx_cm_status  (tenant_id, status),
    KEY idx_cm_person  (tenant_id, sales_person_id),
    -- Idempotensi sweep: satu entri per (sale, rule).
    UNIQUE KEY uq_cm_sale_rule (tenant_id, sale_record_id, rule_id),
    CONSTRAINT chk_cm_status CHECK (
        status IN ('calculated','approved','payable','paid','cancelled','clawed_back')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 3) Akun Utang Komisi 2-6200 utk semua tenant existing (idempoten; pola 2-2200).
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '2-6200', 'Utang Komisi', a.type, a.normal_balance, 1,
       'Kewajiban komisi sales yang sudah diakru (Increment 9, blueprint §3). Dr saat dibayar/di-reverse.',
       1, a.category
FROM accounts a
WHERE a.code = '2-1000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '2-6200');
