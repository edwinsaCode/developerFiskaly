-- P0-4 — True-up HPP sebagai proses closing (runs + lines).
--
-- True-up merekonsiliasi HPP budgeted (saat BAST) ke biaya AKTUAL saat proyek
-- selesai. BUKAN fungsi langsung: ada state machine + approval sebelum posting.
--
-- hpp_trueup_runs.status (IMPL-3): draft → calculated → approved → posted (+ cancelled)
--   - calculate & approve TIDAK membuat jurnal
--   - hanya approved→posted membuat jurnal; posted immutable
--
-- Idempotensi (TU-5 / IMPL-4):
--   - `active_scope` generated: non-cancelled → COALESCE(phase_id,0), cancelled → NULL.
--     UNIQUE (tenant, project, active_scope) → maksimal SATU run non-cancelled per scope.
--   - `journal_id` UNIQUE → satu run maksimal satu jurnal (double-post tak menggandakan).
--   Lapisan ketiga (row-lock saat transisi approved→posted) ditegakkan di service (P2).
--
-- Basis konsisten (IMPL-1): `allocation_config_version_id` = satu version basis
--   untuk seluruh scope; service menolak scope multi-version (ErrMixedAllocationBasisVersion).
-- Audit actor (IMPL-5): created/calculated/approved/posted/cancelled _by + _at.

CREATE TABLE hpp_trueup_runs (
    id                           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id                    BIGINT UNSIGNED NOT NULL,
    project_id                   BIGINT UNSIGNED NOT NULL,
    phase_id                     BIGINT UNSIGNED NULL,
    status                       VARCHAR(20)     NOT NULL DEFAULT 'draft'
                                     COMMENT 'draft|calculated|approved|posted|cancelled',
    allocation_config_version_id BIGINT UNSIGNED NULL COMMENT 'satu version basis utk seluruh scope (IMPL-1)',
    budget_hpp_total             DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    actual_cost_total            DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    variance_total               DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',

    created_by                   BIGINT UNSIGNED NULL,
    calculated_at                DATETIME(3)     NULL,
    calculated_by                BIGINT UNSIGNED NULL,
    approved_at                  DATETIME(3)     NULL,
    approved_by                  BIGINT UNSIGNED NULL,
    posted_at                    DATETIME(3)     NULL,
    posted_by                    BIGINT UNSIGNED NULL,
    cancelled_at                 DATETIME(3)     NULL,
    cancelled_by                 BIGINT UNSIGNED NULL,
    journal_id                   BIGINT UNSIGNED NULL COMMENT 'jurnal adjustment; diisi sekali saat posted',

    created_at                   DATETIME(3)     NULL,
    updated_at                   DATETIME(3)     NULL,

    -- non-cancelled → phase_key; cancelled → NULL (boleh banyak run cancelled).
    active_scope                 BIGINT UNSIGNED AS (
                                     CASE WHEN status = 'cancelled' THEN NULL
                                          ELSE COALESCE(phase_id, 0) END
                                 ) STORED,

    PRIMARY KEY (id),
    INDEX idx_tur_tenant  (tenant_id),
    INDEX idx_tur_project (tenant_id, project_id),
    UNIQUE KEY uq_tur_active_scope (tenant_id, project_id, active_scope),
    UNIQUE KEY uq_tur_journal (tenant_id, journal_id),

    CONSTRAINT chk_tur_status CHECK (status IN ('draft','calculated','approved','posted','cancelled'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='P0-4 true-up run (closing process); satu non-cancelled per scope';

CREATE TABLE hpp_trueup_lines (
    id                 BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id          BIGINT UNSIGNED NOT NULL,
    run_id             BIGINT UNSIGNED NOT NULL,
    unit_id            BIGINT UNSIGNED NOT NULL,
    unit_name_snapshot VARCHAR(100)    NOT NULL DEFAULT '' COMMENT 'nama unit disalin dari snapshot (historis)',
    category           VARCHAR(20)     NOT NULL COMMENT 'accounting_class: land|hard|soft|financing',
    is_sold            TINYINT(1)      NOT NULL DEFAULT 0 COMMENT '1=unit terjual (di-jurnal); 0=unsold (act_HPP finalized, no journal)',
    budgeted_amount    DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    actual_amount      DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    variance_amount    DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'actual - budgeted',
    snapshot_id        BIGINT UNSIGNED NULL COMMENT 'logical link allocation_snapshots (asal budgeted)',
    journal_entry_id   BIGINT UNSIGNED NULL COMMENT 'jurnal adjustment; NULL utk unsold / variance 0',

    created_at         DATETIME(3)     NULL,
    updated_at         DATETIME(3)     NULL,

    PRIMARY KEY (id),
    INDEX idx_tul_tenant (tenant_id),
    INDEX idx_tul_run    (tenant_id, run_id),
    UNIQUE KEY uq_tul_run_unit_cat (tenant_id, run_id, unit_id, category),

    CONSTRAINT fk_tul_run FOREIGN KEY (run_id)
        REFERENCES hpp_trueup_runs (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='P0-4 true-up rincian per (unit, accounting_class); finalized act_HPP semua unit';
