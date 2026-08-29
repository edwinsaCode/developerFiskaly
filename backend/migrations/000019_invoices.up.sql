-- Invoice sequences: per-tenant atomic counter for invoice numbering.
CREATE TABLE invoice_sequences (
    tenant_id BIGINT UNSIGNED NOT NULL,
    next_val  BIGINT UNSIGNED NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id)
);

CREATE TABLE invoices (
    id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    sale_contract_id BIGINT UNSIGNED NOT NULL,
    schedule_id      BIGINT UNSIGNED NULL,
    invoice_number   VARCHAR(30) NOT NULL,
    invoice_type     VARCHAR(20) NOT NULL,
    issue_date       DATE NOT NULL,
    due_date         DATE NOT NULL,
    amount           DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
    status           VARCHAR(20) NOT NULL DEFAULT 'issued',
    notes            TEXT NULL,
    created_by       BIGINT UNSIGNED NOT NULL,
    created_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_invoices_tenant_id (tenant_id),
    INDEX idx_invoices_contract (tenant_id, sale_contract_id),
    INDEX idx_invoices_schedule (tenant_id, schedule_id),
    UNIQUE KEY uk_invoices_number (tenant_id, invoice_number)
);
