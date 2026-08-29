-- FE-3 · P1 — Buyer Credit Lifecycle schema.
--
-- Saldo kredit buyer = POOL FUNGIBLE per unit (bukan milik satu termin). "Apply
-- credit" adalah reklasifikasi SUB-LEDGER (tanpa jurnal, tanpa kas): menandai
-- bagian Uang Muka yang menutup sebuah cicilan. Event apply dicatat di
-- credit_applications (audit who/when/why + idempotency); efek sub-ledger adalah
-- baris payment_allocations bertipe 'credit_application'.
--
-- INVARIAN FE-3:
--   * Tidak ada journal entry / cash movement pada apply-credit.
--   * Append-only: hanya INSERT baris alokasi (cache paid_amount tetap turunan).
--   * available_credit(unit) = Σ(buyer_credit) − Σ(credit_application)
--   * paid_amount(S)         = Σ(schedule + credit_application untuk S)

-- Kredit tidak berasal dari satu termin → termin_payment_id NULL untuk baris
-- credit_application. Baris kas TETAP non-null; guard #3 (uk_pa_termin_target)
-- tak terpengaruh karena schedule_key generated hanya bergantung payment_schedule_id.
ALTER TABLE payment_allocations
    MODIFY COLUMN termin_payment_id BIGINT UNSIGNED NULL,
    ADD COLUMN credit_application_id BIGINT UNSIGNED NULL AFTER termin_payment_id,
    ADD INDEX idx_pa_credit_app (tenant_id, credit_application_id);

CREATE TABLE credit_applications (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    sale_contract_id    BIGINT UNSIGNED NOT NULL,
    unit_id             BIGINT UNSIGNED NOT NULL,
    payment_schedule_id BIGINT UNSIGNED NOT NULL,
    amount              DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    reason              VARCHAR(500)    NOT NULL DEFAULT '',   -- "why"
    applied_by          BIGINT UNSIGNED NULL,                  -- "who"
    idempotency_key     VARCHAR(64)     NULL,                  -- anti double-submit
    created_at          DATETIME(3),                           -- "when"
    updated_at          DATETIME(3),

    INDEX idx_ca_tenant   (tenant_id),
    INDEX idx_ca_contract (tenant_id, sale_contract_id),
    INDEX idx_ca_schedule (tenant_id, payment_schedule_id),
    UNIQUE KEY uk_ca_idempotency (tenant_id, idempotency_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='FE-3: event pemakaian saldo kredit buyer (tanpa jurnal); efek di payment_allocations';
