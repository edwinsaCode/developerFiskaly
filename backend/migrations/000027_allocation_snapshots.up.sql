-- P0-2/P0-3 — Persist HPP allocation snapshot at BAST (Budgeted Cost Allocation).
--
-- Sebelum migrasi ini, HPP unit hanya dihitung runtime dari jurnal (metode
-- "actual") dan disimpan sebagai empat kolom nominal di sale_records — tanpa
-- jejak SUMBER angka itu (RAB versi berapa, basis apa, denominator apa).
-- Metode Budgeted Cost Allocation (docs/budgeted-cost-allocation-spec.md)
-- menurunkan HPP dari RAB yang disetujui; angka itu HARUS auditable dan
-- reproducible. Tabel ini membekukan (freeze) input+output alokasi pada saat
-- BAST sehingga HPP tidak pernah berubah walau RAB kemudian di-supersede.
--
-- INVARIAN (ditegakkan di service layer + integration test):
--   * Immutable   : baris allocation_snapshots + _lines APPEND-ONLY. Tidak
--                   pernah UPDATE/DELETE (Invariant #5, sejalan ledger).
--   * BCA-1       : Σ(allocation_snapshot_lines.amount per class, seluruh unit
--                   di plan yang sama) == pool RAB kapitalisasi per class.
--   * RAB-freeze  : budget_plan_id + budget_plan_version membekukan versi RAB
--                   yang dipakai. Supersede RAB TIDAK mengubah snapshot lama.
--   * Atomicity   : ditulis dalam transaksi BAST yang sama dengan jurnal
--                   Event 3+4 + update unit + sale_record. Tidak ada HPP
--                   budgeted yang terposting tanpa snapshot penyertanya.
--
-- Backward compatibility (freeze §4): sale_records lama TIDAK direkomputasi.
--   hpp_method default 'actual' → semua baris existing otomatis 'actual',
--   allocation_snapshot_id NULL = HPP legacy (actual accumulated cost).
--   Metode 'budgeted' hanya untuk BAST setelah migrasi ini.

CREATE TABLE allocation_snapshots (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    project_id          BIGINT UNSIGNED NOT NULL,
    phase_id            BIGINT UNSIGNED NULL,
    unit_id             BIGINT UNSIGNED NOT NULL,              -- unit yang di-BAST (logical FK → units.id)
    budget_plan_id      BIGINT UNSIGNED NOT NULL,              -- RAB versi yang dibekukan (logical FK → budget_plans.id)
    budget_plan_version INT             NOT NULL DEFAULT 1,    -- snapshot versi RAB (audit walau plan_id di-reuse)
    basis               VARCHAR(20)     NOT NULL,              -- 'saleable_area' | 'sales_value'
    hpp_total           DECIMAL(20,4)   NOT NULL DEFAULT '0.0000', -- Σ lines = HPP budgeted unit ini
    created_at          DATETIME(3),
    updated_at          DATETIME(3),

    INDEX idx_alloc_snap_tenant  (tenant_id),
    INDEX idx_alloc_snap_project (tenant_id, project_id),
    INDEX idx_alloc_snap_plan    (tenant_id, budget_plan_id),

    -- Satu snapshot per unit (satu BAST per unit; sale_records.unit_id juga unik).
    UNIQUE KEY uk_alloc_snap_unit (tenant_id, unit_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='P0-3 snapshot alokasi HPP budgeted saat BAST; append-only (Invariant #5)';

CREATE TABLE allocation_snapshot_lines (
    id                     BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id              BIGINT UNSIGNED NOT NULL,
    snapshot_id            BIGINT UNSIGNED NOT NULL,           -- FK fisik → allocation_snapshots.id
    accounting_class       VARCHAR(20)     NOT NULL,           -- domain.CostCategory: land|hard|soft|financing
    inventory_account_code VARCHAR(20)     NOT NULL,           -- 1-3xxx (denormalized dari taxonomy, audit)
    amount                 DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    created_at             DATETIME(3),
    updated_at             DATETIME(3),

    INDEX idx_alloc_snap_line_snap (tenant_id, snapshot_id),

    -- Maksimal satu baris per (snapshot, accounting_class).
    UNIQUE KEY uk_alloc_snap_line_class (tenant_id, snapshot_id, accounting_class),

    CONSTRAINT fk_asl_snapshot FOREIGN KEY (snapshot_id)
        REFERENCES allocation_snapshots (id) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='P0-3 rincian snapshot HPP per accounting_class (taxonomy = source of truth)';

-- ── sale_records: metode HPP + referensi snapshot RAB ────────────────────────
ALTER TABLE sale_records
    ADD COLUMN hpp_method             VARCHAR(20)     NOT NULL DEFAULT 'actual' AFTER hpp_financing,
    ADD COLUMN allocation_snapshot_id BIGINT UNSIGNED NULL                      AFTER hpp_method,
    ADD COLUMN budget_plan_id         BIGINT UNSIGNED NULL                      AFTER allocation_snapshot_id,
    ADD COLUMN budget_plan_version    INT             NULL                      AFTER budget_plan_id,
    ADD INDEX idx_sale_records_snapshot (tenant_id, allocation_snapshot_id);
