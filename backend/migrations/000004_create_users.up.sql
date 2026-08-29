-- Migration: create users table.
-- Users belong to exactly one tenant and carry a role within that tenant.
-- Email is globally unique: it is the login identifier.
-- Defense-in-depth invariant: queries are scoped via WHERE tenant_id = ?
-- in the application layer (MySQL has no RLS).
CREATE TABLE IF NOT EXISTS users (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id     BIGINT UNSIGNED NOT NULL,
    email         VARCHAR(200)    NOT NULL,
    password_hash VARCHAR(72)     NOT NULL,
    role          VARCHAR(20)     NOT NULL DEFAULT 'viewer' COMMENT 'owner|accountant|viewer',
    created_at    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at    DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    UNIQUE KEY uq_users_email (email),
    KEY idx_users_tenant_id (tenant_id),

    CONSTRAINT fk_users_tenant FOREIGN KEY (tenant_id) REFERENCES tenants (id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;
