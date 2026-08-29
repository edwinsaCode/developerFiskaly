CREATE TABLE accounting_periods (
    id         BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id  BIGINT UNSIGNED NOT NULL,
    year       SMALLINT UNSIGNED NOT NULL,
    month      TINYINT UNSIGNED NOT NULL,
    status     VARCHAR(10) NOT NULL DEFAULT 'open',
    closed_at  DATETIME(3) NULL,
    closed_by  BIGINT UNSIGNED NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    INDEX idx_accounting_periods_tenant_id (tenant_id),
    UNIQUE KEY uk_period_tenant_year_month (tenant_id, year, month)
);
