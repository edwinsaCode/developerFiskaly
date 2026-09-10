-- CLIENT FINAL NOTE (2026-09-10) — Booking Fee boleh Rp0.
--
-- Fee 0 berarti tidak ada uang booking sama sekali (murni reservasi unit):
-- TANPA jurnal kas, TANPA termin, TANPA kwitansi. termin_payment_id jadi
-- NULLable untuk mengakomodasi kasus ini; booking dengan fee > 0 tetap WAJIB
-- punya termin (unchanged, lihat CreateBookingAtomic).

ALTER TABLE bookings
    MODIFY COLUMN termin_payment_id BIGINT UNSIGNED NULL
    COMMENT 'termin penerimaan fee (jurnal + kwitansi); NULL bila booking_fee = 0 (tidak ada uang diterima)';
