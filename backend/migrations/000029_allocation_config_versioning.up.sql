-- P0-4 (D1) — Versioning basis alokasi.
--
-- Basis alokasi (saleable_area | sales_value) menjadi IMMUTABLE untuk sebuah
-- proyek setelah snapshot BAST pertama dibuat (Decision D1). Perubahan basis
-- hanya boleh lewat VERSION baru (deliberate, audited). Setiap allocation
-- snapshot akan menunjuk (pin) version yang dipakai saat BAST (migrasi 000030).
--
-- Pendekatan ADDITIVE (IMPL-2): tabel BARU `allocation_config_versions` —
-- `allocation_configs` lama TIDAK diubah (tetap menyimpan "basis aktif saat ini"
-- untuk kompatibilitas; version = source of truth historis). Satu baris version
-- aktif per proyek (pola one-active seperti budget_plans, Invariant #8).
--
-- Backfill: setiap allocation_configs existing → satu version 1 aktif.

CREATE TABLE allocation_config_versions (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    project_id  BIGINT UNSIGNED NOT NULL,
    version     INT             NOT NULL DEFAULT 1,
    basis       VARCHAR(20)     NOT NULL COMMENT 'saleable_area | sales_value',
    -- active_key = 'Y' saat aktif, NULL saat superseded. MySQL mengizinkan banyak
    -- NULL dalam UNIQUE → hanya satu 'Y' per proyek (one-active guard).
    active_key  CHAR(1)         NULL,
    created_by  BIGINT UNSIGNED NULL COMMENT 'audit: siapa membuat version',
    created_at  DATETIME(3)     NULL,
    updated_at  DATETIME(3)     NULL,

    PRIMARY KEY (id),
    INDEX idx_acv_tenant  (tenant_id),
    INDEX idx_acv_project (tenant_id, project_id),
    UNIQUE KEY uq_acv_version (tenant_id, project_id, version),
    UNIQUE KEY uq_acv_active  (tenant_id, project_id, active_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='P0-4 D1: versi basis alokasi per proyek; snapshot pin version dipakai saat BAST';

-- Backfill: satu version 1 aktif per allocation_configs existing.
INSERT INTO allocation_config_versions (tenant_id, project_id, version, basis, active_key, created_at, updated_at)
SELECT tenant_id, project_id, 1, basis, 'Y', NOW(3), NOW(3)
FROM allocation_configs;
