-- Migration: create units table.
-- Each unit belongs to a project and optionally a phase.
-- Per-category costs live in unit_cost_snapshots (Phase 5 migration).
-- sale_date and sale_price are populated at BAST (Phase 7).

CREATE TABLE IF NOT EXISTS units (
    id            BIGINT UNSIGNED  NOT NULL AUTO_INCREMENT,
    tenant_id     BIGINT UNSIGNED  NOT NULL,
    project_id    BIGINT UNSIGNED  NOT NULL,
    phase_id      BIGINT UNSIGNED  NULL,
    code          VARCHAR(50)      NOT NULL  COMMENT 'e.g. LITHOS-A01; unique per project',
    unit_type     VARCHAR(50)      NOT NULL  COMMENT 'e.g. villa, ruko, apartemen',
    saleable_area DECIMAL(20,4)    NOT NULL DEFAULT '0.0000' COMMENT 'sqm — NOT rupiah',
    list_price    DECIMAL(20,4)    NOT NULL DEFAULT '0.0000' COMMENT 'rupiah bulat (Invariant #2)',
    status        VARCHAR(20)      NOT NULL DEFAULT 'available'
                      COMMENT 'available|reserved|sold',
    buyer_ref     VARCHAR(200)     NULL      COMMENT 'buyer name or CRM reference; set when reserved',
    sale_date     DATE             NULL      COMMENT 'BAST date; set when sold (Phase 7)',
    sale_price    DECIMAL(20,4)    NULL      COMMENT 'rupiah bulat; set at BAST (Phase 7)',
    created_at    DATETIME(3)      NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at    DATETIME(3)      NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_units_tenant_id  (tenant_id),
    KEY idx_units_project_id (project_id),
    KEY idx_units_phase_id   (phase_id),

    -- Enforce unique unit code within a project (across all phases).
    UNIQUE KEY uq_units_project_code (project_id, code),

    CONSTRAINT fk_units_project FOREIGN KEY (project_id)
        REFERENCES projects (id),
    CONSTRAINT fk_units_phase FOREIGN KEY (phase_id)
        REFERENCES project_phases (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
