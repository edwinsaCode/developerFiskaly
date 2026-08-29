-- Phase 7: PPh Final Pengalihan
-- Tabel tarif bertanggal (tidak ada konstanta tarif di kode posting).

CREATE TABLE IF NOT EXISTS tax_rates (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id      BIGINT UNSIGNED NOT NULL,
    rate_code      VARCHAR(50)     NOT NULL COMMENT 'pph_final_pengalihan | ...',
    rate           DECIMAL(10,6)   NOT NULL COMMENT '0.025000 = 2,5% PP 34/2016',
    effective_from DATETIME(3)     NOT NULL COMMENT 'tarif berlaku mulai tanggal ini',
    description    VARCHAR(500)    NOT NULL DEFAULT '',
    created_at     DATETIME(3)     NULL,
    updated_at     DATETIME(3)     NULL,
    PRIMARY KEY (id),
    INDEX idx_tax_rates_tenant  (tenant_id),
    INDEX idx_tax_rates_lookup  (tenant_id, rate_code, effective_from)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Kewajiban pajak per transaksi (Event 5a: Dr 5-2000 / Cr 2-4000).
-- Rate di-snapshot saat akrual — perubahan tarif masa depan tidak berpengaruh.

CREATE TABLE IF NOT EXISTS tax_obligations (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    unit_id          BIGINT UNSIGNED NULL,
    project_id       BIGINT UNSIGNED NULL,
    rate_code        VARCHAR(50)     NOT NULL,
    transfer_value   DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    rate             DECIMAL(10,6)   NOT NULL COMMENT 'snapshot tarif saat akrual',
    tax_amount       DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    status           VARCHAR(20)     NOT NULL DEFAULT 'outstanding'
                     COMMENT 'outstanding | paid',
    accrual_date     DATETIME(3)     NOT NULL,
    journal_entry_id BIGINT UNSIGNED NOT NULL,
    created_at       DATETIME(3)     NULL,
    updated_at       DATETIME(3)     NULL,
    PRIMARY KEY (id),
    INDEX idx_tax_obligations_tenant   (tenant_id),
    INDEX idx_tax_obligations_unit     (tenant_id, unit_id),
    INDEX idx_tax_obligations_period   (tenant_id, accrual_date),
    INDEX idx_tax_obligations_journal  (journal_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- Pembayaran pajak (Event 5b: Dr 2-4000 / Cr Bank).

CREATE TABLE IF NOT EXISTS tax_payments (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    obligation_id    BIGINT UNSIGNED NOT NULL,
    bank_account_code VARCHAR(20)    NOT NULL,
    amount           DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    payment_date     DATETIME(3)     NOT NULL,
    journal_entry_id BIGINT UNSIGNED NOT NULL,
    created_at       DATETIME(3)     NULL,
    updated_at       DATETIME(3)     NULL,
    PRIMARY KEY (id),
    INDEX idx_tax_payments_tenant      (tenant_id),
    INDEX idx_tax_payments_obligation  (obligation_id),
    INDEX idx_tax_payments_journal     (journal_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
