-- Phase 7: SaleContract + PaymentSchedule
-- Tenant-scoped (Invariant #6); kolom uang DECIMAL(20,4) (Invariant #2).

CREATE TABLE sale_contracts (
    id            BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id     BIGINT UNSIGNED NOT NULL,
    unit_id       BIGINT UNSIGNED NOT NULL,
    buyer_name    VARCHAR(200)    NOT NULL DEFAULT '',
    buyer_id      VARCHAR(100)    NOT NULL DEFAULT '',
    payment_type  VARCHAR(20)     NOT NULL DEFAULT 'tunai',
    bank_kpr      VARCHAR(100)    DEFAULT NULL,
    loan_amount   DECIMAL(20,4)   DEFAULT NULL,
    contract_date DATETIME(3)     NOT NULL,
    total_price   DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    created_at    DATETIME(3),
    updated_at    DATETIME(3),
    INDEX idx_sale_contracts_tenant (tenant_id),
    INDEX idx_sale_contracts_unit   (unit_id, tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE payment_schedules (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    sale_contract_id    BIGINT UNSIGNED NOT NULL,
    unit_id             BIGINT UNSIGNED NOT NULL,
    installment_number  INT UNSIGNED    NOT NULL DEFAULT 0,
    due_date            DATETIME(3)     NOT NULL,
    amount              DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    type                VARCHAR(20)     NOT NULL DEFAULT 'installment',
    status              VARCHAR(20)     NOT NULL DEFAULT 'scheduled',
    termin_payment_id   BIGINT UNSIGNED DEFAULT NULL,
    received_at         DATETIME(3)     DEFAULT NULL,
    created_at          DATETIME(3),
    updated_at          DATETIME(3),
    INDEX idx_ps_tenant     (tenant_id),
    INDEX idx_ps_contract   (sale_contract_id),
    INDEX idx_ps_unit       (unit_id, tenant_id),
    INDEX idx_ps_due_date   (due_date),
    INDEX idx_ps_status_due (status, due_date)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
