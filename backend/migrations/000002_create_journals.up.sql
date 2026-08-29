-- Migration: create journal_entries and journal_lines tables.
-- Implements the append-only double-entry ledger (Invariant #5).
-- posted_at IS NOT NULL means the entry is immutable.

CREATE TABLE IF NOT EXISTS journal_entries (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    date        DATE            NOT NULL,
    description VARCHAR(500)    NOT NULL DEFAULT '',
    reference   VARCHAR(100)    NOT NULL DEFAULT '',
    posted_at   DATETIME(3)     NULL     COMMENT 'NULL = draft; NOT NULL = posted (immutable)',
    is_reversing TINYINT(1)     NOT NULL DEFAULT 0,
    reverses_id BIGINT UNSIGNED NULL     COMMENT 'FK to journal_entries.id this entry reverses',
    created_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_je_tenant_id   (tenant_id),
    KEY idx_je_tenant_date (tenant_id, date),
    KEY idx_je_posted_at   (posted_at),
    KEY idx_je_reverses_id (reverses_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;


CREATE TABLE IF NOT EXISTS journal_lines (
    id               BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    tenant_id        BIGINT UNSIGNED  NOT NULL,
    journal_entry_id BIGINT UNSIGNED  NOT NULL,
    account_id       BIGINT UNSIGNED  NOT NULL,
    debit            DECIMAL(20, 4)   NOT NULL DEFAULT 0.0000 COMMENT 'Invariant: debit XOR credit > 0 per line',
    credit           DECIMAL(20, 4)   NOT NULL DEFAULT 0.0000,
    project_id       BIGINT UNSIGNED  NULL     COMMENT 'Optional tag for project-level reporting',
    phase_id         BIGINT UNSIGNED  NULL     COMMENT 'Optional tag for phase-level reporting',
    unit_id          BIGINT UNSIGNED  NULL     COMMENT 'Optional tag for unit-level reporting',
    description      VARCHAR(500)     NOT NULL DEFAULT '',
    created_at       DATETIME(3)      NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3)      NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_jl_journal_entry_id (journal_entry_id),
    KEY idx_jl_tenant_id        (tenant_id),
    KEY idx_jl_account_id       (account_id),
    KEY idx_jl_project_id       (project_id),
    KEY idx_jl_unit_id          (unit_id),

    CONSTRAINT fk_jl_journal_entry FOREIGN KEY (journal_entry_id)
        REFERENCES journal_entries (id),
    CONSTRAINT fk_jl_account FOREIGN KEY (account_id)
        REFERENCES accounts (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
