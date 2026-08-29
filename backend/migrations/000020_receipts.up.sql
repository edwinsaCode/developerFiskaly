-- Receipt sequences: per-tenant atomic counter for receipt (kwitansi) numbering.
CREATE TABLE receipt_sequences (
    tenant_id BIGINT UNSIGNED NOT NULL,
    next_val  BIGINT UNSIGNED NOT NULL DEFAULT 1,
    PRIMARY KEY (tenant_id)
);

-- Receipts (Kwitansi): bukti penerimaan pembayaran. Satu receipt per transaksi
-- termin (termin_payment). Receipt TIDAK memposting jurnal — jurnal sudah
-- diposting saat termin diterima (Dr Bank / Cr Uang Muka Penjualan).
CREATE TABLE receipts (
    id                BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id         BIGINT UNSIGNED NOT NULL,
    termin_payment_id BIGINT UNSIGNED NOT NULL,
    unit_id           BIGINT UNSIGNED NOT NULL,
    sale_contract_id  BIGINT UNSIGNED NULL,
    receipt_number    VARCHAR(30) NOT NULL,
    amount            DECIMAL(20,4) NOT NULL DEFAULT '0.0000',
    bank_account_code VARCHAR(20) NOT NULL,
    received_at       DATETIME(3) NOT NULL,
    notes             VARCHAR(500) NULL,
    created_by        BIGINT UNSIGNED NOT NULL,
    created_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_receipts_tenant_id (tenant_id),
    INDEX idx_receipts_unit (tenant_id, unit_id),
    -- Duplicate prevention: maksimal satu receipt per transaksi termin per tenant.
    UNIQUE KEY uk_receipts_termin (tenant_id, termin_payment_id),
    UNIQUE KEY uk_receipts_number (tenant_id, receipt_number)
);
