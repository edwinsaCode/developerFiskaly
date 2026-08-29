-- Migration: create cost_entries table.
-- Each cost entry records one capitalized development expense (Event 1, posting-rules.md).
-- The associated journal entry (journal_entry_id) is the source of truth for the amount.
-- Biaya TIDAK masuk akun beban (5-xxxx) — hanya ke Persediaan Real Estat (1-3xxx).
--
-- unit_id = NULL  → biaya project-wide (dialokasikan ke unit di Phase 5)
-- unit_id = X     → biaya langsung ke unit X (sudah teratribusi)

CREATE TABLE IF NOT EXISTS cost_entries (
    id                BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    tenant_id         BIGINT UNSIGNED  NOT NULL,
    project_id        BIGINT UNSIGNED  NOT NULL,
    unit_id           BIGINT UNSIGNED  NULL     COMMENT 'NULL = project-wide; NOT NULL = direct unit cost',
    phase_id          BIGINT UNSIGNED  NULL,
    category          VARCHAR(20)      NOT NULL COMMENT 'land|hard|soft|financing',
    amount            DECIMAL(20,4)    NOT NULL DEFAULT '0.0000'
                          COMMENT 'rupiah bulat (Invariant #2); copy dari jurnal untuk display',
    payment_method    VARCHAR(20)      NOT NULL COMMENT 'bank|payable',
    bank_account_code VARCHAR(20)      NOT NULL DEFAULT ''
                          COMMENT 'kode akun bank (1-13xx); kosong jika payment_method = payable',
    date              DATE             NOT NULL,
    vendor            VARCHAR(200)     NOT NULL DEFAULT '',
    description       VARCHAR(500)     NOT NULL DEFAULT '',
    journal_entry_id  BIGINT UNSIGNED  NOT NULL
                          COMMENT 'FK ke journal_entries; sumber kebenaran finansial',
    created_at        DATETIME(3)      NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at        DATETIME(3)      NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_ce_tenant_id          (tenant_id),
    KEY idx_ce_tenant_project     (tenant_id, project_id),
    KEY idx_ce_tenant_unit        (tenant_id, unit_id),
    KEY idx_ce_journal_entry_id   (journal_entry_id),
    KEY idx_ce_phase_id           (phase_id),

    CONSTRAINT fk_ce_project FOREIGN KEY (project_id)
        REFERENCES projects (id),
    CONSTRAINT fk_ce_journal FOREIGN KEY (journal_entry_id)
        REFERENCES journal_entries (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
