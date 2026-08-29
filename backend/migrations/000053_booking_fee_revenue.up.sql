-- BUSINESS RULE FINAL KLIEN (2026-07-29) — Booking Fee = PENDAPATAN BOOKING.
-- Menggantikan R4 Opsi A (fee sebagai liability + disposisi manual).
--
-- Saat fee diterima: Dr Kas/Bank / Cr 4-2100 Pendapatan Booking (langsung
-- pendapatan, final). Tidak ada refund, tidak ada reversal saat batal, tidak
-- pernah direklas ke harga/DP/buyer credit. Histori lama TIDAK disentuh
-- (append-only): baris booking lama tetap di jalur 2-2100 legacy.

-- 1) Akun 4-2100 Pendapatan Booking untuk semua tenant existing (idempoten;
--    pola 000045 §5 — clone atribut dari akun revenue 4-2000).
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '4-2100', 'Pendapatan Booking', a.type, a.normal_balance, 1,
       'Pendapatan booking fee — diakui LANGSUNG saat fee diterima (rule klien 2026-07-29). Akun tersendiri; tidak pernah direklas ke Penjualan Rumah, tidak ada reversal saat booking batal.',
       1, a.category
FROM accounts a
WHERE a.code = '4-2000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '4-2100');

-- 2) Disposisi baru 'recognized': fee sudah diakui pendapatan sejak diterima —
--    FINAL di seluruh lifecycle (active/converted/expired/cancelled semuanya sah).
ALTER TABLE bookings DROP CHECK chk_bookings_disposition;
ALTER TABLE bookings DROP CHECK chk_bookings_status_disposition;
ALTER TABLE bookings
    ADD CONSTRAINT chk_bookings_disposition CHECK (
        fee_disposition IN ('held','transferred','forfeited','pending_refund','refunded','recognized')
    ),
    ADD CONSTRAINT chk_bookings_status_disposition CHECK (
        -- Kebijakan BARU: recognized sah di semua status (pendapatan final).
        fee_disposition = 'recognized'
        -- Jalur LEGACY (baris histori pra-rule): kombinasi lama tetap sah.
     OR (status = 'active'    AND fee_disposition = 'held')
     OR (status = 'converted' AND fee_disposition IN ('transferred','held','forfeited','pending_refund','refunded'))
     OR (status IN ('expired','cancelled') AND fee_disposition IN ('forfeited','pending_refund','refunded'))
    );
