-- Rollback R4: kembali ke perilaku pra-R4 (semua termin dihitung ke harga).
-- Aman: rumus SumTerminsByUnit fallback ke "semua termin" saat kolom hilang.

ALTER TABLE bookings DROP CHECK chk_bookings_status_disposition;
ALTER TABLE bookings
    ADD CONSTRAINT chk_bookings_status_disposition CHECK (
        (status = 'active'    AND fee_disposition = 'held')
     OR (status = 'converted' AND fee_disposition = 'transferred')
     OR (status IN ('expired','cancelled') AND fee_disposition IN ('forfeited','pending_refund','refunded'))
    );

DROP INDEX idx_tp_counts ON termin_payments;

ALTER TABLE termin_payments
    DROP COLUMN counts_toward_price;
