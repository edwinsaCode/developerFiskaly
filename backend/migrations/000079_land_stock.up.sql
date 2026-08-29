-- LT-3 (kelebihan-tanah-final-architecture §B.1) — land_stock: pool
-- inventory Kelebihan Tanah per proyek. Tabel baru murni, tidak ada
-- pembaca lain yang bergantung padanya — additive, reversible.
--
-- available_quantity_m2 TIDAK disimpan — selalu total - reserved - sold saat
-- dibaca, supaya tidak ada dua sumber kebenaran (SSOT).

CREATE TABLE IF NOT EXISTS land_stock (
    id                    BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id             BIGINT UNSIGNED NOT NULL,
    project_id            BIGINT UNSIGNED NOT NULL,
    product_code          VARCHAR(64)     NOT NULL DEFAULT 'kelebihan_tanah',
    total_quantity_m2     DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'input manual admin, tanpa validasi silang ke projects.land_area (tidak reliable)',
    reserved_quantity_m2  DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'counter, diubah transaksional (LT-4)',
    sold_quantity_m2      DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'counter, diubah transaksional (LT-5)',
    unit_price            DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'harga per m2 saat ini; default reservasi baru, bukan sumber kebenaran transaksi',
    created_at            DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at            DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    -- satu pool per proyek (§B.1) — juga berfungsi sebagai index tenant_id.
    UNIQUE KEY uq_land_stock_tenant_project (tenant_id, project_id),

    CONSTRAINT fk_land_stock_project FOREIGN KEY (project_id) REFERENCES projects (id),

    CONSTRAINT chk_land_stock_nonnegative CHECK (
        total_quantity_m2 >= 0 AND reserved_quantity_m2 >= 0 AND sold_quantity_m2 >= 0 AND unit_price >= 0
    ),
    -- INV-LAND-1 (§J): reserved + sold tidak pernah melebihi total.
    CONSTRAINT chk_land_stock_capacity CHECK (
        reserved_quantity_m2 + sold_quantity_m2 <= total_quantity_m2
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
