-- Increment 8 — Cancellation & Refund (blueprint §1, CORE).
--
-- Cancellation membatalkan penjualan (pra/pasca-BAST) secara BENAR:
--   pra-BAST : Dr Uang Muka (Σ diterima) / Cr Pendapatan Lain (penalti)
--                                        / Cr Hutang Refund (sisa)
--   pasca-BAST: reversing Pendapatan (Event 3) + SELURUH COGS unit
--               (INV-COGS-SUM: Event 4 + porsi true-up) + PPh Final accrual,
--               lalu settlement Uang Muka yang sama seperti pra-BAST.
-- Refund = dokumen pembayaran keluar: Dr Hutang Refund 2-2200 / Cr Bank.
-- Semua append-only via jurnal baru/pembalik (Invariant #5); saldo derived.

-- 1) Dokumen cancellation. Lifecycle blueprint: requested → approved →
--    processed | rejected. Satu cancellation AKTIF (non-terminal-gagal) per
--    unit via active_key (pola bookings/budget_plans).
CREATE TABLE IF NOT EXISTS cancellations (
    id               BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id        BIGINT UNSIGNED NOT NULL,
    project_id       BIGINT UNSIGNED NOT NULL,
    unit_id          BIGINT UNSIGNED NOT NULL,
    sale_contract_id BIGINT UNSIGNED NULL COMMENT 'NULL = penjualan unit-only legacy (termin tanpa kontrak)',
    sale_record_id   BIGINT UNSIGNED NULL COMMENT 'terisi utk stage post_bast',
    stage            VARCHAR(10)     NOT NULL COMMENT 'pre_bast | post_bast (dideteksi saat request)',
    reason           VARCHAR(500)    NOT NULL DEFAULT '',
    event_date       DATETIME(3)     NOT NULL COMMENT 'tanggal kejadian pembatalan (bisnis)',
    penalty          DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'penalti/forfeit → 4-2000',
    received_total   DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'Σ diterima (snapshot saat processed; derived dari ledger 2-2000 per unit)',
    refund_amount    DECIMAL(20,4)   NOT NULL DEFAULT '0.0000' COMMENT 'received_total − penalty',
    status           VARCHAR(20)     NOT NULL DEFAULT 'requested',

    requested_by  BIGINT UNSIGNED NULL,
    approved_at   DATETIME(3)     NULL,
    approved_by   BIGINT UNSIGNED NULL,
    rejected_at   DATETIME(3)     NULL,
    rejected_by   BIGINT UNSIGNED NULL,
    reject_reason VARCHAR(500)    NOT NULL DEFAULT '',
    processed_at  DATETIME(3)     NULL,
    processed_by  BIGINT UNSIGNED NULL,

    -- Jejak jurnal (audit; NULL sebelum processed / bila tak berlaku).
    revenue_reversal_journal_id BIGINT UNSIGNED NULL,
    cogs_reversal_journal_id    BIGINT UNSIGNED NULL,
    tax_reversal_journal_id     BIGINT UNSIGNED NULL,
    settlement_journal_id       BIGINT UNSIGNED NULL,
    refund_id                   BIGINT UNSIGNED NULL,

    -- Aktif = requested|approved ('Y'); processed/rejected → NULL.
    active_key CHAR(1)     NULL,
    created_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at DATETIME(3) NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_cx_tenant  (tenant_id),
    KEY idx_cx_unit    (tenant_id, unit_id),
    KEY idx_cx_status  (tenant_id, status),
    UNIQUE KEY uq_cx_one_active (tenant_id, unit_id, active_key),
    CONSTRAINT fk_cx_unit FOREIGN KEY (unit_id) REFERENCES units (id),
    CONSTRAINT chk_cx_stage  CHECK (stage IN ('pre_bast','post_bast')),
    CONSTRAINT chk_cx_status CHECK (status IN ('requested','approved','rejected','processed'))
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 2) Dokumen refund (pembayaran keluar). Sumber: cancellation ATAU booking
--    (fee pending_refund dari Increment 7).
CREATE TABLE IF NOT EXISTS refunds (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id       BIGINT UNSIGNED NOT NULL,
    source_type     VARCHAR(20)     NOT NULL COMMENT 'cancellation | booking',
    cancellation_id BIGINT UNSIGNED NULL,
    booking_id      BIGINT UNSIGNED NULL,
    unit_id         BIGINT UNSIGNED NOT NULL,
    payee           VARCHAR(200)    NOT NULL DEFAULT '' COMMENT 'nama penerima (snapshot)',
    amount          DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    status          VARCHAR(20)     NOT NULL DEFAULT 'pending',
    -- payable_journal_id: reklas sumber → 2-2200 (booking: Dr 2-2100/Cr 2-2200;
    -- cancellation: NULL — payable sudah dikredit di settlement).
    payable_journal_id BIGINT UNSIGNED NULL,
    payment_journal_id BIGINT UNSIGNED NULL COMMENT 'Dr 2-2200 / Cr Bank saat dibayar',
    bank_account_code  VARCHAR(20)     NOT NULL DEFAULT '',
    paid_at            DATETIME(3)     NULL,
    paid_by            BIGINT UNSIGNED NULL,
    notes              VARCHAR(500)    NOT NULL DEFAULT '',
    created_by         BIGINT UNSIGNED NULL,
    created_at         DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at         DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_rf_tenant (tenant_id),
    KEY idx_rf_status (tenant_id, status),
    KEY idx_rf_unit   (tenant_id, unit_id),
    CONSTRAINT chk_rf_source CHECK (source_type IN ('cancellation','booking')),
    CONSTRAINT chk_rf_status CHECK (status IN ('pending','paid','cancelled')),
    CONSTRAINT chk_rf_source_ref CHECK (
        (source_type = 'cancellation' AND cancellation_id IS NOT NULL)
     OR (source_type = 'booking'      AND booking_id IS NOT NULL)
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 3) sale_records: penanda pembatalan pasca-BAST (append-only: baris BAST tetap;
--    kolom penanda + link dokumen pembatalan).
ALTER TABLE sale_records
    ADD COLUMN cancelled_at    DATETIME(3)     NULL,
    ADD COLUMN cancellation_id BIGINT UNSIGNED NULL;

-- 4) bookings: disposisi baru 'refunded' (fee pending_refund yang sudah
--    dibayarkan via Refund). MySQL: ganti CHECK.
ALTER TABLE bookings DROP CHECK chk_bookings_disposition;
ALTER TABLE bookings DROP CHECK chk_bookings_status_disposition;
ALTER TABLE bookings
    ADD CONSTRAINT chk_bookings_disposition CHECK (
        fee_disposition IN ('held','transferred','forfeited','pending_refund','refunded')
    ),
    ADD CONSTRAINT chk_bookings_status_disposition CHECK (
        (status = 'active'    AND fee_disposition = 'held')
     OR (status = 'converted' AND fee_disposition = 'transferred')
     OR (status IN ('expired','cancelled') AND fee_disposition IN ('forfeited','pending_refund','refunded'))
    );

-- 5) Akun Hutang Refund 2-2200 untuk semua tenant existing (idempoten; pola 2-2100).
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '2-2200', 'Hutang Refund', a.type, a.normal_balance, 1,
       'Kewajiban pengembalian dana buyer (blueprint §1). Dikredit saat cancellation processed / reklas titipan booking; didebit saat refund dibayar.',
       1, a.category
FROM accounts a
WHERE a.code = '2-2000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '2-2200');
