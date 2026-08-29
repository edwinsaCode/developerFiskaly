-- Increment 7 — Booking (blueprint §2, CORE).
--
-- Booking = pemesanan unit ber-booking-fee SEBELUM PPJB; konsumen pertama Unit
-- Lifecycle (Increment 6). Fee = uang titipan (KEWAJIBAN, bukan pendapatan):
--   terima : Dr Bank / Cr Titipan Booking 2-2100
--   konversi: Dr Titipan Booking / Cr Uang Muka 2-2000 (fee jadi bagian DP)
--   hangus : Dr Titipan Booking / Cr Pendapatan Lain-lain 4-2000 (forfeit)
-- Refund cash-out = domain Refund (Increment 8) — di sini hanya pending_refund.
-- Additive + reversible. Semua saldo derived dari ledger (tanpa tabel saldo).

-- 1) Tabel bookings.
CREATE TABLE IF NOT EXISTS bookings (
    id                    BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    tenant_id             BIGINT UNSIGNED NOT NULL,
    project_id            BIGINT UNSIGNED NOT NULL,
    unit_id               BIGINT UNSIGNED NOT NULL,
    phase_id              BIGINT UNSIGNED NULL,
    customer_id           BIGINT UNSIGNED NOT NULL COMMENT 'wajib (kebijakan Increment 1/3)',
    sales_person_id       BIGINT UNSIGNED NULL COMMENT 'atribusi funnel/KPI (blueprint §4)',
    lead_id               BIGINT UNSIGNED NULL COMMENT 'SEAM Lead/CRM (blueprint §2) — tanpa FK; tabel lead belum ada',
    booking_fee           DECIMAL(20,4)   NOT NULL DEFAULT '0.0000',
    refundable            TINYINT(1)      NOT NULL DEFAULT 0 COMMENT '0=hangus saat expired/cancel (forfeit→4-2000); 1=pending_refund (cash-out Increment 8)',
    booking_date          DATETIME(3)     NOT NULL,
    expiry_date           DATETIME(3)     NOT NULL,
    status                VARCHAR(20)     NOT NULL DEFAULT 'active',
    -- fee_disposition: posisi uang titipan. held (di 2-2100) | transferred (reklas
    -- ke 2-2000 saat konversi) | forfeited (ke 4-2000) | pending_refund (tertahan
    -- di 2-2100 menunggu domain Refund).
    fee_disposition       VARCHAR(20)     NOT NULL DEFAULT 'held',
    termin_payment_id     BIGINT UNSIGNED NOT NULL COMMENT 'termin penerimaan fee (jurnal + kwitansi)',
    converted_contract_id BIGINT UNSIGNED NULL,
    forfeit_journal_id    BIGINT UNSIGNED NULL COMMENT 'jurnal forfeit (bila hangus)',
    reclass_journal_id    BIGINT UNSIGNED NULL COMMENT 'jurnal reklas Titipan→Uang Muka (bila konversi)',
    close_reason          VARCHAR(500)    NOT NULL DEFAULT '',
    closed_at             DATETIME(3)     NULL COMMENT 'waktu pencatatan terminal (converted/expired/cancelled)',
    closed_event_date     DATETIME(3)     NULL COMMENT 'tanggal KEJADIAN bisnis terminal (pelajaran 5.1)',
    closed_by             BIGINT UNSIGNED NULL,
    notes                 VARCHAR(500)    NOT NULL DEFAULT '',
    created_by            BIGINT UNSIGNED NULL,
    -- Satu booking AKTIF per unit: active_key='Y' saat active, NULL saat terminal
    -- (pola budget_plans/approval_requests — NULL boleh banyak dalam UNIQUE).
    active_key            CHAR(1)         NULL,
    created_at            DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3),
    updated_at            DATETIME(3)     NOT NULL DEFAULT CURRENT_TIMESTAMP(3) ON UPDATE CURRENT_TIMESTAMP(3),

    PRIMARY KEY (id),
    KEY idx_bookings_tenant   (tenant_id),
    KEY idx_bookings_unit     (tenant_id, unit_id),
    KEY idx_bookings_customer (tenant_id, customer_id),
    -- Sweep expiry: cari booking active yang lewat expiry.
    KEY idx_bookings_expiry   (tenant_id, status, expiry_date),
    UNIQUE KEY uq_bookings_one_active (tenant_id, unit_id, active_key),

    CONSTRAINT fk_bookings_unit     FOREIGN KEY (unit_id)     REFERENCES units (id),
    CONSTRAINT fk_bookings_customer FOREIGN KEY (customer_id) REFERENCES customers (id),

    CONSTRAINT chk_bookings_status CHECK (
        status IN ('active','converted','expired','cancelled')
    ),
    CONSTRAINT chk_bookings_disposition CHECK (
        fee_disposition IN ('held','transferred','forfeited','pending_refund')
    ),
    -- Konsistensi status × disposisi: active=held; converted=transferred;
    -- expired/cancelled = forfeited|pending_refund.
    CONSTRAINT chk_bookings_status_disposition CHECK (
        (status = 'active'    AND fee_disposition = 'held')
     OR (status = 'converted' AND fee_disposition = 'transferred')
     OR (status IN ('expired','cancelled') AND fee_disposition IN ('forfeited','pending_refund'))
    )
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- 2) Akun Titipan Booking (2-2100) untuk SEMUA tenant existing — idempoten.
--    Kolom type/normal_balance/category disalin dari 2-2000 (kewajiban, kredit)
--    agar konsisten per tenant. Tenant baru mendapatkannya via SeedCOA.
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '2-2100', 'Titipan Booking', a.type, a.normal_balance, 1,
       'Uang titipan booking fee sebelum PPJB (kewajiban; blueprint §2). Konversi → reklas ke 2-2000; hangus → 4-2000.',
       1, a.category
FROM accounts a
WHERE a.code = '2-2000'
  AND NOT EXISTS (
      SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '2-2100'
  );

-- 3) Aktifkan 4-2000 Pendapatan Lain-lain (tujuan forfeit) — sudah ada di seed
--    tapi non-aktif untuk tenant lama.
UPDATE accounts SET is_active = 1 WHERE code = '4-2000';
