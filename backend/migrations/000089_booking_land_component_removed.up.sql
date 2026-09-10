-- kelebihan-tanah-konversi-kontrak-2026-08: Kelebihan Tanah dipindahkan dari
-- Booking ke Konversi Kontrak (instruksi klien 2026-08-31) — salesperson tidak
-- lagi memilih komponen tanah saat Booking sama sekali; satu-satunya titik
-- masuk sekarang adalah Konversi Kontrak (sale_contracts.land_* — TIDAK
-- disentuh migrasi ini, kolomnya tetap dipakai).
--
-- Kolom bookings.land_* (000084) menjadi tidak terpakai untuk booking baru;
-- dihapus bersih (bukan dibiarkan mati) — CLAUDE.md melarang sisa kolom tak
-- terpakai selamanya kalau memang sudah pasti tidak dipakai lagi.
ALTER TABLE bookings
    DROP CONSTRAINT chk_bookings_land_component,
    DROP FOREIGN KEY fk_bookings_land_stock,
    DROP FOREIGN KEY fk_bookings_land_reservation,
    DROP COLUMN land_stock_id,
    DROP COLUMN land_reservation_id,
    DROP COLUMN land_quantity_m2,
    DROP COLUMN land_unit_price_snapshot;
