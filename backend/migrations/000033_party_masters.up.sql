-- Increment 1 (Foundation Accounting) — Party/Actor Masters.
--
-- Blueprint: erp-blueprint-review.md §1 (Customer master naik dari buyer_ref) +
-- §8 (Sales Person/Team). Domain: party master (Buyer/Customer, Sales Team, Sales
-- Person) — TIDAK menyentuh ledger, tidak ada jurnal. Menyediakan SEAM referensi
-- (nullable customer_id, sales_person_id di sale_contracts) tanpa merombak alur
-- jual existing (IMPL-2 additive): kolom lama buyer_ref/buyer_name/buyer_id tetap.

CREATE TABLE customers (
    id         BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id  BIGINT UNSIGNED NOT NULL,
    code       VARCHAR(50)     NOT NULL,
    name       VARCHAR(200)    NOT NULL,
    type       VARCHAR(20)     NOT NULL DEFAULT 'individual' COMMENT 'individual | company',
    id_number  VARCHAR(50)     NOT NULL DEFAULT '' COMMENT 'NIK/KTP atau no. identitas badan',
    npwp       VARCHAR(30)     NOT NULL DEFAULT '',
    phone      VARCHAR(30)     NOT NULL DEFAULT '',
    email      VARCHAR(120)    NOT NULL DEFAULT '',
    address    VARCHAR(500)    NOT NULL DEFAULT '',
    is_active  TINYINT(1)      NOT NULL DEFAULT 1,
    created_by BIGINT UNSIGNED NULL,
    created_at DATETIME(3)     NULL,
    updated_at DATETIME(3)     NULL,

    PRIMARY KEY (id),
    INDEX idx_cust_tenant (tenant_id),
    UNIQUE KEY uq_cust_code (tenant_id, code),
    CONSTRAINT chk_cust_type CHECK (type IN ('individual','company'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Customer/Buyer master (CORE); menggantikan buyer_ref free-text via seam customer_id';

CREATE TABLE sales_teams (
    id                     BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id              BIGINT UNSIGNED NOT NULL,
    code                   VARCHAR(50)     NOT NULL,
    name                   VARCHAR(150)    NOT NULL,
    leader_sales_person_id BIGINT UNSIGNED NULL COMMENT 'logical ref sales_persons.id',
    is_active              TINYINT(1)      NOT NULL DEFAULT 1,
    created_at             DATETIME(3)     NULL,
    updated_at             DATETIME(3)     NULL,

    PRIMARY KEY (id),
    INDEX idx_steam_tenant (tenant_id),
    UNIQUE KEY uq_steam_code (tenant_id, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Sales Team master (tipis); target/KPI = read-model P2';

CREATE TABLE sales_persons (
    id            BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id     BIGINT UNSIGNED NOT NULL,
    code          VARCHAR(50)     NOT NULL,
    name          VARCHAR(200)    NOT NULL,
    sales_team_id BIGINT UNSIGNED NULL COMMENT 'logical ref sales_teams.id',
    user_id       BIGINT UNSIGNED NULL COMMENT 'logical ref users.id (opsional)',
    phone         VARCHAR(30)     NOT NULL DEFAULT '',
    email         VARCHAR(120)    NOT NULL DEFAULT '',
    join_date     DATE            NULL,
    is_active     TINYINT(1)      NOT NULL DEFAULT 1,
    created_at    DATETIME(3)     NULL,
    updated_at    DATETIME(3)     NULL,

    PRIMARY KEY (id),
    INDEX idx_sp_tenant (tenant_id),
    INDEX idx_sp_team   (tenant_id, sales_team_id),
    UNIQUE KEY uq_sp_code (tenant_id, code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Sales Person master; atribusi penjualan (ref di sale_contracts)';

-- SEAM referensi di sale_contracts (nullable, additive) — belum di-rewire.
ALTER TABLE sale_contracts
    ADD COLUMN customer_id     BIGINT UNSIGNED NULL,
    ADD COLUMN sales_person_id BIGINT UNSIGNED NULL,
    ADD INDEX idx_sc_customer     (tenant_id, customer_id),
    ADD INDEX idx_sc_salesperson  (tenant_id, sales_person_id);
