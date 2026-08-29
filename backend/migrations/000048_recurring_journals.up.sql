-- PS-5 — Jurnal Berulang (recurring journal): TEMPLATE konfigurasi (bukan
-- catatan akuntansi). Hasil eksekusi = jurnal POSTED biasa via posting service
-- (balanced dijamin engine). lines = JSON template (preseden: scheme params).

CREATE TABLE IF NOT EXISTS recurring_journals (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id    BIGINT UNSIGNED NOT NULL,
    name         VARCHAR(200)    NOT NULL,
    description  VARCHAR(500)    NOT NULL DEFAULT '',
    day_of_month INT             NOT NULL DEFAULT 1 COMMENT '1-28 (aman utk semua bulan)',
    `lines`      JSON            NOT NULL COMMENT '[{account_code, debit, credit, description}]',
    is_active    TINYINT(1)      NOT NULL DEFAULT 1,
    last_run_ym  CHAR(7)         NOT NULL DEFAULT '' COMMENT 'YYYY-MM terakhir dieksekusi (guard idempoten per bulan)',
    created_by   BIGINT UNSIGNED NULL,
    created_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at   DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    PRIMARY KEY (id),
    KEY idx_rj_tenant (tenant_id),
    CONSTRAINT chk_rj_day CHECK (day_of_month BETWEEN 1 AND 28)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
