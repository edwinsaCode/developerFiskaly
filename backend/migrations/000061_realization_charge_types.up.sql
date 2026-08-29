-- W-1 — Master Jenis Biaya Realisasi (keputusan bisnis FINAL klien 2026-08-06).
--
-- Sebelum ini sistem punya DUA taksonomi yang bertabrakan: `product_types`
-- memuat `pdam` sebagai produk ber-pendapatan, sementara PDAM sebenarnya
-- TITIPAN; dan label item tagihan ("Notaris", "BPHTB", ...) hanyalah teks bebas
-- tanpa master, sehingga perlakuan akuntansi satu item bergantung pada menu mana
-- yang dipakai admin — bukan pada aturan.
--
-- Keputusan klien:
--   * Produk yang DIJUAL hanya: Rumah, Ruko, Kelebihan Tanah.
--   * Notaris, BPHTB, PDAM, Listrik adalah JENIS BIAYA REALISASI (titipan).
--   * Admin harus bisa mengelola master ini sendiri (CRUD) tanpa programmer.
--
-- Seam domain: domain.BillingTreatment (revenue | deposit_liability) — sejajar
-- domain.ProductCategory. Dua master, satu aturan.

-- ── 1. realization_charge_types — master per tenant ─────────────────────────
CREATE TABLE realization_charge_types (
    id                   BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id            BIGINT UNSIGNED NOT NULL,
    code                 VARCHAR(50)     NOT NULL,
    name                 VARCHAR(200)    NOT NULL,
    -- treatment: seam domain.BillingTreatment. Hari ini seluruh biaya realisasi
    -- = deposit_liability (K-1). Kolom tetap eksplisit supaya aturan terbaca di
    -- data, bukan tersembunyi di kode.
    treatment            VARCHAR(30)     NOT NULL DEFAULT 'deposit_liability',
    -- Akun kewajiban titipan. Default 2-2400 untuk SEMUA jenis: owner menulis
    -- jurnalnya sebagai SATU baris "Titipan Realisasi", bukan satu akun per
    -- vendor. Kolom ini editable bagi tenant yang ingin memisah — dijaga
    -- invariant "satu grup, satu akun titipan" di service.
    deposit_account_code VARCHAR(20)     NOT NULL DEFAULT '2-2400',
    is_active            TINYINT(1)      NOT NULL DEFAULT 1,
    created_at           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at           DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    UNIQUE KEY uq_rct_tenant_code (tenant_id, code),
    INDEX idx_rct_tenant (tenant_id),
    CONSTRAINT chk_rct_treatment CHECK (treatment IN ('revenue','deposit_liability'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Master jenis biaya realisasi (titipan): Notaris/BPHTB/PDAM/Listrik — dikelola admin';

-- ── 2. Seed default untuk seluruh tenant existing (idempoten) ───────────────
-- Sumber daftar tenant = accounts (setiap tenant pasti punya COA hasil seed).
INSERT INTO realization_charge_types (tenant_id, code, name, treatment, deposit_account_code, is_active)
SELECT t.tenant_id, s.code, s.name, 'deposit_liability', '2-2400', 1
FROM (SELECT DISTINCT tenant_id FROM accounts) t
CROSS JOIN (
    SELECT 'notaris' AS code, 'Biaya Notaris'   AS name UNION ALL
    SELECT 'bphtb',           'BPHTB'                    UNION ALL
    SELECT 'pdam',            'Sambungan PDAM'           UNION ALL
    SELECT 'listrik',         'Sambungan Listrik'
) s
WHERE NOT EXISTS (
    SELECT 1 FROM realization_charge_types r
    WHERE r.tenant_id = t.tenant_id AND r.code = s.code
);

-- ── 3. charge_items.charge_type_code — item menunjuk master ────────────────
-- NULL diizinkan supaya baris HISTORIS tetap terbaca (invariant #5: histori
-- tidak ditulis ulang). Item BARU wajib punya kode — ditegakkan fail-closed di
-- service layer, bukan di schema, agar backfill di bawah tidak gagal.
ALTER TABLE charge_items
    ADD COLUMN charge_type_code VARCHAR(50) NULL AFTER label,
    ADD INDEX idx_ci_charge_type (tenant_id, charge_type_code);

-- ── 4. Backfill kode untuk item historis yang labelnya jelas ───────────────
-- Hanya mengisi kolom BARU; tidak menyentuh amount, status, alokasi, atau
-- jurnal mana pun. Label yang tidak dikenali sengaja DIBIARKAN NULL — menebak
-- lebih buruk daripada mengakui tidak tahu.
UPDATE charge_items SET charge_type_code = 'notaris' WHERE charge_type_code IS NULL AND LOWER(label) LIKE '%notaris%';
UPDATE charge_items SET charge_type_code = 'bphtb'   WHERE charge_type_code IS NULL AND LOWER(label) LIKE '%bphtb%';
UPDATE charge_items SET charge_type_code = 'pdam'    WHERE charge_type_code IS NULL AND LOWER(label) LIKE '%pdam%';
UPDATE charge_items SET charge_type_code = 'listrik' WHERE charge_type_code IS NULL AND LOWER(label) LIKE '%listrik%';

-- ── 5. Audit perubahan master data (TD-8) ──────────────────────────────────
-- Mapping akun adalah keputusan finansial: mengubah akun titipan sebuah jenis
-- biaya mengubah ke mana uang customer mendarat. APPEND-ONLY — tidak pernah
-- di-update, tidak pernah dihapus.
CREATE TABLE master_data_changes (
    id          BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id   BIGINT UNSIGNED NOT NULL,
    entity      VARCHAR(64)     NOT NULL,  -- 'realization_charge_type' | 'product_type'
    entity_code VARCHAR(64)     NOT NULL,
    field       VARCHAR(64)     NOT NULL,
    old_value   VARCHAR(255)    NOT NULL DEFAULT '',
    new_value   VARCHAR(255)    NOT NULL DEFAULT '',
    changed_by  BIGINT UNSIGNED NULL,
    created_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at  DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    INDEX idx_mdc_tenant (tenant_id),
    INDEX idx_mdc_entity (tenant_id, entity, entity_code)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='Audit append-only perubahan master data finansial (TD-8)';
