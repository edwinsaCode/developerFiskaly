-- LT-4 (kelebihan-tanah-final-architecture §B.2, §D, §J) — reservasi pool
-- Kelebihan Tanah. Keputusan klien 2026-08-20: TANPA booking fee — reservasi
-- murni soft-lock kuantitas, tanpa transaksi finansial, tanpa jurnal.
--
-- Konkurensi (INV-LAND-1: reserved_quantity_m2 + sold_quantity_m2 <= total_quantity_m2)
-- ditegakkan DUA lapis: row-lock SELECT...FOR UPDATE pada land_stock di service
-- layer (internal/land), dan CHECK constraint chk_land_stock_capacity yang
-- sudah ada di land_stock (000079) sebagai penjaga terakhir di level DB.
--
-- converted_sale_id sengaja TANPA FK ke land_sales di sini — tabel itu baru
-- lahir di LT-5. FK ditambahkan via ALTER pada migrasi LT-5.

CREATE TABLE IF NOT EXISTS land_stock_reservations (
    id                  BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    land_stock_id       BIGINT UNSIGNED NOT NULL,
    project_id          BIGINT UNSIGNED NOT NULL COMMENT 'denormalized dari land_stock.project_id, pola bookings.project_id',
    customer_id         BIGINT UNSIGNED NOT NULL,
    sales_person_id     BIGINT UNSIGNED NULL COMMENT 'atribusi funnel/KPI; tanpa FK, pola bookings.sales_person_id',
    quantity_m2         DECIMAL(20,4) NOT NULL,
    unit_price_snapshot DECIMAL(20,4) NOT NULL DEFAULT '0.0000' COMMENT 'snapshot land_stock.unit_price saat reservasi dibuat',
    reserved_at         DATETIME(3) NOT NULL,
    expiry_date         DATETIME(3) NULL,
    status              VARCHAR(20) NOT NULL DEFAULT 'active',
    converted_sale_id   BIGINT UNSIGNED NULL COMMENT 'terisi saat convert ke land_sales (LT-5); FK ditambahkan di migrasi LT-5',
    cancelled_reason    VARCHAR(500) NOT NULL DEFAULT '',
    created_by          BIGINT UNSIGNED NULL,
    created_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at          DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_land_reservations_tenant    (tenant_id),
    KEY idx_land_reservations_stock     (tenant_id, land_stock_id),
    KEY idx_land_reservations_project   (tenant_id, project_id),
    KEY idx_land_reservations_customer  (tenant_id, customer_id),
    -- Sweep expiry: cari reservasi active yang lewat expiry_date.
    KEY idx_land_reservations_expiry    (tenant_id, status, expiry_date),

    CONSTRAINT fk_land_reservations_stock    FOREIGN KEY (land_stock_id) REFERENCES land_stock (id),
    CONSTRAINT fk_land_reservations_project  FOREIGN KEY (project_id)    REFERENCES projects (id),
    CONSTRAINT fk_land_reservations_customer FOREIGN KEY (customer_id)  REFERENCES customers (id),

    CONSTRAINT chk_land_reservations_status CHECK (
        status IN ('active','converted','expired','cancelled')
    ),
    CONSTRAINT chk_land_reservations_qty_positive CHECK (quantity_m2 > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
