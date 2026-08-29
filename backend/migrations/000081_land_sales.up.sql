-- LT-5 (kelebihan-tanah-final-architecture §B.3, §B.4, §F) — land_sales +
-- land_allocations: Akad-equivalent standalone untuk penjualan Kelebihan
-- Tanah, meniru pola sale_records/allocation_snapshots TANPA memaksa masuk
-- ke sale_contracts.unit_id (instruksi eksplisit owner).
--
-- land_sales: struktur PPN identik sale_contracts (dpp_amount/is_pkp/
--   vat_rate_snapshot/gross_amount) — reuse mesin pajak generik, bukan mesin
--   kedua. payment_account_code BARU (tidak ada di draf desain) — perlu untuk
--   OPEN DECISION §B.3 yang sudah dijawab: v1 tunai/lunas saja, jadi admin
--   memilih akun kas/bank COA tujuan langsung saat Akad (identik pola
--   ValidateCashBankAccount internal/sale), TANPA melibatkan
--   termin_payments/ReceivePayment sama sekali.
--
-- land_allocations: SELALU dibuat 1:1 per land_sale (R-2 "snapshot beku"),
--   berbeda dari allocation_snapshots yang hanya dibuat utk metode budgeted —
--   karena land_sales sendiri TIDAK punya kolom hpp_total/hpp_rate (§B.3),
--   land_allocations adalah satu-satunya tempat angka itu hidup untuk KEDUA
--   metode. budget_plan_id/allocation_config_version_id NULL = metode actual
--   (pola sama seperti allocation_snapshots.budget_plan_id yang NOT NULL di
--   sana justru karena baris itu memang hanya ada utk budgeted).
--
-- FK budget_plan_id/allocation_config_version_id sengaja LOGICAL saja (tanpa
-- CONSTRAINT fisik) — pola identik allocation_snapshots (000027/000030):
-- supersede RAB tidak boleh terganjal FK ke snapshot lama.

CREATE TABLE IF NOT EXISTS land_sales (
    id                   BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id            BIGINT UNSIGNED NOT NULL,
    project_id           BIGINT UNSIGNED NOT NULL,
    land_stock_id        BIGINT UNSIGNED NOT NULL,
    reservation_id       BIGINT UNSIGNED NULL COMMENT 'null bila dijual langsung tanpa reservasi lebih dulu',
    customer_id          BIGINT UNSIGNED NOT NULL,
    sales_person_id      BIGINT UNSIGNED NULL COMMENT 'atribusi funnel/KPI; tanpa FK, pola land_stock_reservations.sales_person_id',
    quantity_m2          DECIMAL(20,4)   NOT NULL,
    unit_price_snapshot  DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'harga per m2 final saat Akad (bukan snapshot reservasi)',
    dpp_amount           DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'DPP / harga dasar sebelum PPN',
    is_pkp               TINYINT(1)      NOT NULL DEFAULT 0 COMMENT '1 = penjual PKP, PPN terutang',
    vat_rate_snapshot    DECIMAL(10,6)   NOT NULL DEFAULT '0.000000' COMMENT 'snapshot tarif PPN saat Akad, 0 utk non-PKP; TODO(tax-advisor) aturan PPN Kelebihan Tanah belum final',
    gross_amount         DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'DPP+PPN (PKP) atau DPP (non-PKP) — tagihan bruto',
    payment_account_code VARCHAR(20)     NOT NULL DEFAULT '' COMMENT 'akun kas/bank COA tujuan penerimaan — v1 tunai/lunas saja (§B.3 OPEN DECISION, dijawab)',
    recognition_date     DATETIME(3)     NULL COMMENT 'terisi saat status=akad; analog Akad — titik pengakuan revenue+HPP',
    status                VARCHAR(20)     NOT NULL DEFAULT 'draft',
    revenue_journal_id   BIGINT UNSIGNED NULL COMMENT 'Event 3 — posted saat Akad',
    cogs_journal_id      BIGINT UNSIGNED NULL COMMENT 'Event 4 — NULL jika HPP=0',
    created_by           BIGINT UNSIGNED NULL,
    created_at           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_land_sales_tenant      (tenant_id),
    KEY idx_land_sales_project     (tenant_id, project_id),
    KEY idx_land_sales_stock       (tenant_id, land_stock_id),
    KEY idx_land_sales_customer    (tenant_id, customer_id),
    KEY idx_land_sales_reservation (tenant_id, reservation_id),
    KEY idx_land_sales_rev_jrn     (revenue_journal_id),
    KEY idx_land_sales_cogs_jrn    (cogs_journal_id),

    CONSTRAINT fk_land_sales_stock       FOREIGN KEY (land_stock_id)  REFERENCES land_stock (id),
    CONSTRAINT fk_land_sales_project     FOREIGN KEY (project_id)     REFERENCES projects (id),
    CONSTRAINT fk_land_sales_customer    FOREIGN KEY (customer_id)    REFERENCES customers (id),
    CONSTRAINT fk_land_sales_reservation FOREIGN KEY (reservation_id) REFERENCES land_stock_reservations (id),

    CONSTRAINT chk_land_sales_status       CHECK (status IN ('draft','akad','cancelled')),
    CONSTRAINT chk_land_sales_qty_positive CHECK (quantity_m2 > 0)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='LT-5: Akad-equivalent standalone Kelebihan Tanah — pengganti sale_contracts.unit_id/charge_groups';

CREATE TABLE IF NOT EXISTS land_allocations (
    id                            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id                     BIGINT UNSIGNED NOT NULL,
    project_id                    BIGINT UNSIGNED NOT NULL,
    land_sale_id                  BIGINT UNSIGNED NOT NULL,
    land_stock_id                 BIGINT UNSIGNED NOT NULL,
    quantity_m2                   DECIMAL(20,4)   NOT NULL COMMENT 'duplikasi land_sales.quantity_m2 saat snapshot dibuat',
    hpp_rate_per_m2_snapshot      DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    hpp_total                     DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'quantity_m2 × hpp_rate_per_m2_snapshot, dibulatkan',
    basis                         VARCHAR(20)     NOT NULL DEFAULT 'land_area',
    allocation_config_version_id  BIGINT UNSIGNED NULL COMMENT 'logical FK → allocation_config_versions.id; NULL = metode actual',
    allocation_config_version     INT             NULL,
    budget_plan_id                BIGINT UNSIGNED NULL COMMENT 'logical FK → budget_plans.id; NULL = metode actual (bukan budgeted)',
    budget_plan_version           INT             NULL,
    created_at                    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at                    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3) COMMENT 'kolom wajib per konvensi tabel; secara desain tidak pernah tersentuh (append-only, R-2)',

    PRIMARY KEY (id),
    UNIQUE KEY uq_land_allocations_sale (tenant_id, land_sale_id),
    KEY idx_land_allocations_tenant  (tenant_id),
    KEY idx_land_allocations_project (tenant_id, project_id),
    KEY idx_land_allocations_stock   (tenant_id, land_stock_id),

    CONSTRAINT fk_land_alloc_sale    FOREIGN KEY (land_sale_id)  REFERENCES land_sales (id),
    CONSTRAINT fk_land_alloc_stock   FOREIGN KEY (land_stock_id) REFERENCES land_stock (id),
    CONSTRAINT fk_land_alloc_project FOREIGN KEY (project_id)    REFERENCES projects (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='LT-5: snapshot HPP Land per land_sale, immutable, wajib 1:1 (R-2)';

-- Antisipasi dari komentar migrasi 000080: FK ditambahkan sekarang karena
-- land_sales baru lahir di migrasi ini.
ALTER TABLE land_stock_reservations
    ADD CONSTRAINT fk_land_reservations_sale FOREIGN KEY (converted_sale_id) REFERENCES land_sales (id);
