CREATE TABLE IF NOT EXISTS allocation_executions (
    id           BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id    BIGINT UNSIGNED NOT NULL,
    project_id   BIGINT UNSIGNED NOT NULL,
    basis        VARCHAR(20)   NOT NULL                        COMMENT 'basis alokasi saat eksekusi',
    executed_by  BIGINT UNSIGNED NOT NULL                      COMMENT 'user_id dari JWT',
    user_email   VARCHAR(200)  NOT NULL DEFAULT ''             COMMENT 'email snapshot saat eksekusi',
    total_cost   DECIMAL(20,4) NOT NULL DEFAULT '0.0000'       COMMENT 'Σ Total HPP semua unit saat eksekusi',
    executed_at  DATETIME(3)   NOT NULL,
    created_at   DATETIME(3),
    updated_at   DATETIME(3),
    INDEX idx_alloc_exec_tenant  (tenant_id),
    INDEX idx_alloc_exec_project (tenant_id, project_id, executed_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
