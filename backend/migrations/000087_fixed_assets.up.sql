-- Fixed Asset — Register + garis penyusutan.
--
-- Register TIDAK menyimpan accumulated_depreciation atau book_value sebagai
-- kolom: keduanya diturunkan dari SUM(fixed_asset_depreciation_lines.amount)
-- setiap kali dibaca, mengikuti prinsip yang sudah berlaku di seluruh sistem
-- ini (accumulated cost proyek/unit juga selalu dihitung ulang dari jurnal,
-- bukan disimpan) — satu sumber kebenaran, tidak ada dua angka yang bisa
-- berselisih.
--
-- asset_code di-generate dari id sendiri (FA-000001 dst) SETELAH baris
-- ter-insert — bukan nomor dokumen legal/fiskal (beda dengan BKK/invoice W-2),
-- jadi tidak memerlukan document numbering engine; ia murni label register.
--
-- INV-FA-1 (idempotensi penyusutan): UNIQUE(tenant_id, fixed_asset_id,
-- period_year, period_month) pada garis penyusutan — satu aset hanya boleh
-- disusutkan SATU KALI per periode, ditegakkan constraint DB sebagai penjaga
-- terakhir di belakang pengecekan aplikasi.

CREATE TABLE fixed_assets (
    id                        BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id                 BIGINT UNSIGNED NOT NULL,
    project_id                BIGINT UNSIGNED NULL COMMENT 'NULL = aset level perusahaan, bukan milik satu proyek',
    category_id               BIGINT UNSIGNED NOT NULL,
    asset_code                VARCHAR(50)     NOT NULL,
    asset_name                VARCHAR(200)    NOT NULL,
    acquisition_date          DATE            NOT NULL,
    acquisition_cost          DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    residual_value            DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    useful_life_months        INT UNSIGNED    NOT NULL,
    depreciation_method       VARCHAR(20)     NOT NULL DEFAULT 'straight_line',
    depreciation_start_date   DATE            NOT NULL COMMENT 'Konvensi v1: sama dengan acquisition_date (mulai bulan perolehan)',
    status                    VARCHAR(20)     NOT NULL DEFAULT 'active' COMMENT 'active | disposed',
    disposed_at               DATETIME(3)     NULL,
    acquisition_journal_id    BIGINT UNSIGNED NOT NULL,
    vendor                    VARCHAR(200)    NULL,
    description               VARCHAR(500)    NULL,
    created_at                DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at                DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    UNIQUE KEY uq_fa_tenant_code (tenant_id, asset_code),
    INDEX idx_fa_tenant (tenant_id),
    INDEX idx_fa_project (tenant_id, project_id),
    INDEX idx_fa_category (category_id),
    INDEX idx_fa_status (tenant_id, status),
    CONSTRAINT fk_fa_category FOREIGN KEY (category_id) REFERENCES fixed_asset_categories (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Fixed Asset Register — satu baris per aset tetap';

CREATE TABLE fixed_asset_depreciation_lines (
    id               BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    fixed_asset_id   BIGINT UNSIGNED NOT NULL,
    period_year      SMALLINT UNSIGNED NOT NULL,
    period_month     TINYINT UNSIGNED NOT NULL,
    amount           DECIMAL(20,4)   NOT NULL,
    journal_entry_id BIGINT UNSIGNED NOT NULL,
    created_at       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at       DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    UNIQUE KEY uq_fadl_asset_period (tenant_id, fixed_asset_id, period_year, period_month),
    INDEX idx_fadl_tenant (tenant_id),
    INDEX idx_fadl_journal (journal_entry_id),
    CONSTRAINT fk_fadl_asset FOREIGN KEY (fixed_asset_id) REFERENCES fixed_assets (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Fixed Asset — garis penyusutan terposting per aset per periode (append-only)';
