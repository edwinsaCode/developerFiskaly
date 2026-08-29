-- Increment 6 — Unit Lifecycle State Machine.
--
-- Menambah audit trail transisi status unit (append-only) + memperluas vocabulary
-- status unit dari 3 (available|reserved|sold) menjadi 9 nilai (blueprint §9).
-- TANPA dampak ledger: status unit = metadata stok; semua angka tetap dari jurnal.
--
-- Prinsip: additive-only, reversible. Nilai lama TIDAK di-rename (kompat penuh
-- reader/data existing). Nilai baru: booked, ppjb, occupied, hold, blocked,
-- maintenance.

-- 1) Tabel audit transisi (APPEND-ONLY — tidak pernah di-update/delete).
--    Koreksi = transisi balik baru. event_date = tanggal KEJADIAN bisnis
--    (pelajaran Increment 5.1), bisa beda dari created_at (waktu pencatatan).
CREATE TABLE IF NOT EXISTS unit_status_transitions (
    id             BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id      BIGINT UNSIGNED NOT NULL,
    unit_id        BIGINT UNSIGNED NOT NULL,
    from_status    VARCHAR(20)     NOT NULL DEFAULT '' COMMENT 'kosong utk baris backfill (titik awal audit)',
    to_status      VARCHAR(20)     NOT NULL,
    event          VARCHAR(40)     NOT NULL COMMENT 'trigger typed: bast_executed, reservation_confirmed, admin_hold, manual, backfill, dst.',
    event_date     DATETIME(3)     NOT NULL COMMENT 'tanggal kejadian bisnis (bisa beda dari created_at)',
    reference_type VARCHAR(30)     NOT NULL DEFAULT 'manual' COMMENT 'booking|sale_contract|sale_record|cancellation|approval_request|manual|backfill (polimorfik)',
    reference_id   BIGINT UNSIGNED NULL,
    actor_id       BIGINT UNSIGNED NULL COMMENT 'user pemicu transisi (NULL utk sistem/backfill)',
    notes          VARCHAR(500)    NOT NULL DEFAULT '',
    created_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at     DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_ust_tenant   (tenant_id),
    KEY idx_ust_unit     (tenant_id, unit_id),
    -- time-on-market (CFO Q41/G6): unit terakhir masuk 'available' → sekarang.
    KEY idx_ust_tom      (tenant_id, to_status, created_at),

    CONSTRAINT fk_ust_unit FOREIGN KEY (unit_id)
        REFERENCES units (id),

    -- Defense-in-depth: to_status wajib salah satu dari 9 nilai kanonik;
    -- from_status boleh kosong (backfill) atau salah satu dari 9.
    CONSTRAINT chk_ust_to_status CHECK (
        to_status IN ('available','booked','reserved','ppjb','sold','occupied','hold','blocked','maintenance')
    ),
    CONSTRAINT chk_ust_from_status CHECK (
        from_status IN ('','available','booked','reserved','ppjb','sold','occupied','hold','blocked','maintenance')
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 2) CHECK units.status (D4) — data existing hanya berisi 3 nilai sah → aman.
--    Kolom VARCHAR(20) existing sudah menampung nilai baru; hanya menambah guard.
ALTER TABLE units
    ADD CONSTRAINT chk_units_status CHECK (
        status IN ('available','booked','reserved','ppjb','sold','occupied','hold','blocked','maintenance')
    );

-- 3) Backfill: satu baris sintetis per unit existing sebagai titik awal audit
--    (from='' to=<status saat ini> event='backfill' event_date=updated_at).
INSERT INTO unit_status_transitions
    (tenant_id, unit_id, from_status, to_status, event, event_date, reference_type, created_at, updated_at)
SELECT tenant_id, id, '', status, 'backfill', updated_at, 'backfill', CURRENT_TIMESTAMP(3), CURRENT_TIMESTAMP(3)
FROM units;
