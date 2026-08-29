-- FE-2 · P1 (schema) — Sub-ledger alokasi pembayaran.
--
-- Memetakan penerimaan uang (termin_payments, 1 jurnal) ke cicilan
-- (payment_schedules) secara many-to-many. Sebelum tabel ini, alokasi hanya
-- dihitung runtime (planAllocation) dan TIDAK dipersist — jejaknya cuma cache
-- payment_schedules.paid_amount + FK termin_payment_id yang diisi hanya saat
-- cicilan lunas (lossy untuk partial). Tabel ini menutup celah itu.
--
-- INVARIAN (ditegakkan di service layer + integration test, mulai P2):
--   * Konservasi   : Σ(amount WHERE termin_payment_id=X) == termin_payments.amount
--   * Rekonsiliasi : payment_schedules.paid_amount == Σ(amount WHERE
--                    payment_schedule_id=S AND allocation_type='schedule')
--   * No-journal    : penulisan baris ini TIDAK pernah memposting jurnal
--                    (Invariant #1/#5). Alokasi = sub-ledger murni.
--   * Atomicity     : ditulis dalam transaksi yang sama dengan termin+jurnal+
--                    kwitansi (P2). Tidak boleh ada jurnal tanpa alokasi.
--
-- Keputusan desain (disetujui 2026-07-01):
--   * FK logical saja (tenant-scoped, ikut pola migrasi existing) — TIDAK ada
--     FOREIGN KEY fisik pada fase awal.
--   * Buyer credit (kelebihan bayar pra-BAST) = baris dengan
--     allocation_type='buyer_credit' dan payment_schedule_id NULL — bukan tabel
--     terpisah.

CREATE TABLE payment_allocations (
    id                  BIGINT UNSIGNED AUTO_INCREMENT PRIMARY KEY,
    tenant_id           BIGINT UNSIGNED NOT NULL,
    termin_payment_id   BIGINT UNSIGNED NOT NULL,            -- sumber uang (logical FK → termin_payments.id)
    payment_schedule_id BIGINT UNSIGNED NULL,                -- target cicilan (logical FK → payment_schedules.id); NULL = buyer credit
    allocation_type     VARCHAR(20)     NOT NULL DEFAULT 'schedule', -- 'schedule' | 'buyer_credit'
    amount              DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',   -- selalu > 0 (baris nol tidak dibuat)
    created_by          BIGINT UNSIGNED NULL,                -- audit "siapa input"
    created_at          DATETIME(3),
    updated_at          DATETIME(3),

    -- Kunci alokasi ternormalisasi (guard #3): NULL (buyer_credit) dipetakan ke 0
    -- agar UNIQUE menjangkau baris buyer_credit — MySQL memperlakukan NULL sebagai
    -- distinct sehingga UNIQUE biasa TIDAK bisa mencegah duplikat buyer_credit.
    -- STORED agar bisa diindeks (MySQL 8.0). Tidak dipetakan di struct GORM.
    schedule_key        BIGINT UNSIGNED AS (COALESCE(payment_schedule_id, 0)) STORED,

    INDEX idx_pa_tenant   (tenant_id),
    INDEX idx_pa_termin   (tenant_id, termin_payment_id),
    INDEX idx_pa_schedule (tenant_id, payment_schedule_id),

    -- Satu index menegakkan DUA invarian guard #3 sekaligus:
    --   * schedule    : maksimal satu baris per (tenant, termin, cicilan)
    --   * buyer_credit : schedule_key=0 → maksimal satu buyer_credit per (tenant, termin)
    UNIQUE KEY uk_pa_termin_target (tenant_id, termin_payment_id, schedule_key)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci
  COMMENT='FE-2 sub-ledger: alokasi termin→cicilan; tanpa jurnal (Invariant #1/#5)';
