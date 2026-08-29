-- Phase 5: Allocation Config
-- Menyimpan basis alokasi HPP per proyek (saleable_area atau sales_value).
-- 1 baris per proyek (unique constraint), diperbarui saat tenant ganti basis.

CREATE TABLE IF NOT EXISTS allocation_configs (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    project_id  BIGINT UNSIGNED NOT NULL,
    basis       VARCHAR(20)     NOT NULL COMMENT 'saleable_area | sales_value',
    created_at  DATETIME(3)     NULL,
    updated_at  DATETIME(3)     NULL,

    PRIMARY KEY (id),
    UNIQUE KEY uq_allocation_configs_project (tenant_id, project_id),
    INDEX idx_allocation_configs_tenant (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Basis alokasi HPP per proyek — Phase 5 Engine Alokasi';
