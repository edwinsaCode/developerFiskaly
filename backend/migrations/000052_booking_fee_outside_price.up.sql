-- R4 — Booking Fee di LUAR harga unit (keputusan PO 2026-07-29, design
-- docs/r4-booking-fee-outside-price-design.md, Core + Opsi A).
--
-- SATU source of truth: flag `counts_toward_price` pada termin_payments —
-- penanda "apakah pembayaran ini mengurangi harga rumah".
--   DEFAULT TRUE  → SELURUH baris histori identik (append-only, nol backfill).
--   Booking fee BARU di-insert FALSE → fee tidak pernah mengurangi outstanding.

ALTER TABLE termin_payments
    ADD COLUMN counts_toward_price BOOLEAN NOT NULL DEFAULT TRUE;

CREATE INDEX idx_tp_counts ON termin_payments (tenant_id, unit_id, counts_toward_price);

-- Opsi A: booking outside-price yang dikonversi TETAP 'held' di 2-2100 sampai
-- disposisi manual (forfeit / refund). Longgarkan CHECK status↔disposisi:
--   converted + transferred        → jalur LAMA (fee reklas ke uang muka)
--   converted + held               → jalur BARU (fee menetap, menunggu disposisi)
--   converted + forfeited/refund*  → disposisi fee pasca-konversi (Opsi A)
ALTER TABLE bookings DROP CHECK chk_bookings_status_disposition;
ALTER TABLE bookings
    ADD CONSTRAINT chk_bookings_status_disposition CHECK (
        (status = 'active'    AND fee_disposition = 'held')
     OR (status = 'converted' AND fee_disposition IN ('transferred','held','forfeited','pending_refund','refunded'))
     OR (status IN ('expired','cancelled') AND fee_disposition IN ('forfeited','pending_refund','refunded'))
    );
