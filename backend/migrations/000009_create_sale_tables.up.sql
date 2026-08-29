-- Phase 6: Penjualan & Pengakuan Pendapatan
-- termin_payments: Event 2 — penerimaan uang muka/termin sebelum BAST.
--   Dr Bank (1-13xx) / Cr Uang Muka Penjualan (2-2000). Bukan pendapatan (Invariant #7).
-- sale_records: Event 3+4 — snapshot BAST; RevenueJournalID + COGSJournalID selalu dipasang atomik.

CREATE TABLE IF NOT EXISTS termin_payments (
    id                BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    tenant_id         BIGINT UNSIGNED  NOT NULL,
    unit_id           BIGINT UNSIGNED  NOT NULL,
    project_id        BIGINT UNSIGNED  NOT NULL,
    phase_id          BIGINT UNSIGNED  NULL,
    amount            DECIMAL(20,4)    NOT NULL DEFAULT '0.0000',
    bank_account_code VARCHAR(20)      NOT NULL,
    date              DATETIME(3)      NOT NULL,
    description       VARCHAR(500)     NOT NULL DEFAULT '',
    journal_entry_id  BIGINT UNSIGNED  NOT NULL,
    created_at        DATETIME(3)      NULL,
    updated_at        DATETIME(3)      NULL,

    PRIMARY KEY (id),
    INDEX idx_termin_payments_tenant      (tenant_id),
    INDEX idx_termin_payments_unit        (tenant_id, unit_id),
    INDEX idx_termin_payments_project     (tenant_id, project_id),
    INDEX idx_termin_payments_journal     (journal_entry_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Event 2: penerimaan termin/uang muka sebelum BAST (kewajiban, bukan pendapatan)';

CREATE TABLE IF NOT EXISTS sale_records (
    id                    BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    tenant_id             BIGINT UNSIGNED  NOT NULL,
    unit_id               BIGINT UNSIGNED  NOT NULL,
    project_id            BIGINT UNSIGNED  NOT NULL,
    phase_id              BIGINT UNSIGNED  NULL,
    sale_price            DECIMAL(20,4)    NOT NULL DEFAULT '0.0000' COMMENT 'DPP sebelum PPN',
    is_vat                TINYINT(1)       NOT NULL DEFAULT 0,
    vat_rate              DECIMAL(10,6)    NOT NULL DEFAULT '0.000000',
    total_advance_at_bast DECIMAL(20,4)    NOT NULL DEFAULT '0.0000'
                          COMMENT 'snapshot Σ termin saat BAST — immutable',
    hpp_land              DECIMAL(20,4)    NOT NULL DEFAULT '0.0000',
    hpp_hard              DECIMAL(20,4)    NOT NULL DEFAULT '0.0000',
    hpp_soft              DECIMAL(20,4)    NOT NULL DEFAULT '0.0000',
    hpp_financing         DECIMAL(20,4)    NOT NULL DEFAULT '0.0000',
    buyer_ref             VARCHAR(200)     NOT NULL DEFAULT '',
    bast_date             DATETIME(3)      NOT NULL,
    revenue_journal_id    BIGINT UNSIGNED  NOT NULL  COMMENT 'Event 3 journal — posted, immutable',
    cogs_journal_id       BIGINT UNSIGNED  NULL      COMMENT 'Event 4 journal — NULL jika HPP=0',
    created_at            DATETIME(3)      NULL,
    updated_at            DATETIME(3)      NULL,

    PRIMARY KEY (id),
    UNIQUE KEY uq_sale_records_unit (tenant_id, unit_id) COMMENT 'satu unit hanya satu BAST',
    INDEX idx_sale_records_tenant  (tenant_id),
    INDEX idx_sale_records_project (tenant_id, project_id),
    INDEX idx_sale_records_rev_jrn (revenue_journal_id),
    INDEX idx_sale_records_cogs_jrn (cogs_journal_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Event 3+4: snapshot BAST — pendapatan + HPP per kategori, atomik, immutable';
