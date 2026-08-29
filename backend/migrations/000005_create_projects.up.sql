-- Migration: create projects and project_phases tables.
-- Projects accumulate capitalized development costs (Phase 5).
-- Phases subdivide a project for per-phase reporting.

CREATE TABLE IF NOT EXISTS projects (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    name        VARCHAR(200)    NOT NULL,
    status      VARCHAR(20)     NOT NULL DEFAULT 'planning'
                    COMMENT 'planning|active|selling|completed',
    start_date  DATE            NULL,
    land_area   DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'sqm — NOT rupiah',
    notes       VARCHAR(2000)   NOT NULL DEFAULT '',
    created_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_projects_tenant_id (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;


CREATE TABLE IF NOT EXISTS project_phases (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id    BIGINT UNSIGNED NOT NULL,
    project_id   BIGINT UNSIGNED NOT NULL,
    name         VARCHAR(100)    NOT NULL,
    description  VARCHAR(500)    NOT NULL DEFAULT '',
    target_units INT             NOT NULL DEFAULT 0,
    status       VARCHAR(20)     NOT NULL DEFAULT 'planning'
                     COMMENT 'planning|active|completed',
    created_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_pp_tenant_id  (tenant_id),
    KEY idx_pp_project_id (project_id),

    CONSTRAINT fk_pp_project FOREIGN KEY (project_id)
        REFERENCES projects (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
