-- R3 — Progress FISIK proyek (operasional, BUKAN ledger, bukan duplicate SoT).
-- Append-only: koreksi = entri baru dengan nilai benar, bukan edit/hapus.
CREATE TABLE project_progress_entries (
    id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id    BIGINT UNSIGNED NOT NULL,
    project_id   BIGINT UNSIGNED NOT NULL,
    phase_id     BIGINT UNSIGNED NULL,
    progress_pct DECIMAL(5,2)    NOT NULL,
    as_of_date   DATE            NOT NULL,
    notes        VARCHAR(500)    NOT NULL DEFAULT '',
    created_by   BIGINT UNSIGNED NULL,
    created_at   DATETIME(3),
    updated_at   DATETIME(3),
    INDEX idx_ppe_tenant (tenant_id),
    INDEX idx_ppe_project (tenant_id, project_id, as_of_date),
    CONSTRAINT chk_ppe_pct CHECK (progress_pct >= 0 AND progress_pct <= 100)
);
