-- Migration: create accounts table
-- Chart of Accounts for multi-tenant real-estate developer system.
-- tenant_id is the primary isolation boundary (no MySQL RLS; enforced in app layer).

CREATE TABLE IF NOT EXISTS accounts (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id      BIGINT UNSIGNED NOT NULL,
    code           VARCHAR(20)     NOT NULL,
    name           VARCHAR(200)    NOT NULL,
    type           VARCHAR(20)     NOT NULL COMMENT 'asset|liability|equity|revenue|expense',
    normal_balance VARCHAR(10)     NOT NULL COMMENT 'debit|credit',
    is_system      TINYINT(1)      NOT NULL DEFAULT 0,
    description    TEXT            NOT NULL DEFAULT '',
    created_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    -- Enforce unique code per tenant (not globally unique).
    UNIQUE KEY uq_accounts_tenant_code (tenant_id, code),
    -- Fast tenant scans.
    KEY idx_accounts_tenant_id (tenant_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
