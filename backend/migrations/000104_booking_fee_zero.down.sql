-- Rollback: kembalikan termin_payment_id ke NOT NULL. Aman hanya bila tidak
-- ada baris booking_fee = 0 yang sudah dibuat (termin_payment_id NULL) —
-- best-effort seperti pola migration lain.

ALTER TABLE bookings
    MODIFY COLUMN termin_payment_id BIGINT UNSIGNED NOT NULL
    COMMENT 'termin penerimaan fee (jurnal + kwitansi)';
