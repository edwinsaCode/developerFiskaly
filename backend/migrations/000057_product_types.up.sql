-- UAT Batch 2 §2 — Product Catalog: developer menjual PRODUK (rumah, ruko,
-- kavling, kelebihan tanah, PDAM, ...), bukan hanya "rumah". Master per tenant
-- + mapping akun pendapatan per produk (COA-driven, editable — tidak hardcode).
-- Additive: tabel units TIDAK di-rename; units.unit_type = kode product type
-- (baris lama bebas — fallback 4-1000 di posting, kompat histori).

CREATE TABLE product_types (
    id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id            BIGINT UNSIGNED NOT NULL,
    code                 VARCHAR(50)  NOT NULL,
    name                 VARCHAR(200) NOT NULL,
    -- property: ikut lifecycle unit + HPP/alokasi; non_property: barang/jasa
    -- sederhana (revenue saat serah terima, tanpa pool HPP).
    category             VARCHAR(20)  NOT NULL DEFAULT 'property',
    revenue_account_code VARCHAR(20)  NOT NULL DEFAULT '4-1000',
    is_active            BOOLEAN      NOT NULL DEFAULT TRUE,
    created_at           DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at           DATETIME(3)  NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),
    CONSTRAINT chk_product_category CHECK (category IN ('property','non_property')),
    UNIQUE KEY uq_product_types_code (tenant_id, code),
    INDEX idx_product_types_tenant (tenant_id)
);

-- Seed default per tenant existing (idempoten). Mapping akun default:
-- property → 4-1000; non_property → 4-2000 (Pendapatan Lain-lain) — EDITABLE.
-- TODO(tax-advisor): akun & perlakuan PPN untuk kelebihan tanah / PDAM.
INSERT INTO product_types (tenant_id, code, name, category, revenue_account_code)
SELECT t.id, s.code, s.name, s.category, s.acct
FROM tenants t
JOIN (
    SELECT 'rumah'           AS code, 'Rumah'            AS name, 'property'     AS category, '4-1000' AS acct
    UNION ALL SELECT 'ruko',            'Ruko',              'property',     '4-1000'
    UNION ALL SELECT 'kavling',         'Kavling',           'property',     '4-1000'
    UNION ALL SELECT 'kelebihan_tanah', 'Kelebihan Tanah',   'non_property', '4-2000'
    UNION ALL SELECT 'pdam',            'Sambungan PDAM',    'non_property', '4-2000'
) s
WHERE NOT EXISTS (
    SELECT 1 FROM product_types p WHERE p.tenant_id = t.id AND p.code = s.code
);
