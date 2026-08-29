-- Booking-embedded Kelebihan Tanah (kelebihan-tanah-booking-integration-2026-08).
--
-- Perubahan kebijakan bisnis (instruksi klien 2026-08-20): "Kelebihan Tanah"
-- BUKAN LAGI sebuah halaman jual terpisah yang harus dikunjungi salesperson
-- (LT-8 membekukan jalur addon lama, bukan mengganti jalur baru ini). Sekarang
-- dipilih sebagai komponen OPSIONAL langsung di form Booking/Konversi — quantity
-- saja diinput, harga & HPP otomatis dari internal/land (LandStock.UnitPrice /
-- LandHPPResolver). Reservasi dibuat ATOMIK di dalam transaksi booking yang sama
-- (land.ReserveTx, land.CloseReservationTx, land.RecordAkadTx — lihat
-- internal/land/repository.go) — bukan engine kedua, memakai ulang LT-1..8 apa
-- adanya, hanya titik pemicunya yang berubah dari "kelola tersendiri" menjadi
-- "menempel pada lifecycle Booking→Kontrak→Akad unit".
--
-- Kolom semuanya NULLABLE: booking/kontrak TANPA komponen tanah (mayoritas
-- kasus) punya NULL di keempatnya — tidak mengubah perilaku lama sama sekali.
ALTER TABLE bookings
    ADD COLUMN land_stock_id             BIGINT UNSIGNED NULL COMMENT 'Kelebihan Tanah proyek ini, bila booking menyertakan komponen tanah' AFTER unit_id,
    ADD COLUMN land_reservation_id       BIGINT UNSIGNED NULL COMMENT 'reservasi land_stock_reservations dibuat atomik bersama booking (land.ReserveTx)' AFTER land_stock_id,
    ADD COLUMN land_quantity_m2          DECIMAL(20,4)   NULL COMMENT 'm2 yang dipesan salesperson; harga TIDAK diinput manual' AFTER land_reservation_id,
    ADD COLUMN land_unit_price_snapshot  DECIMAL(20,4)   NULL COMMENT 'snapshot land_stock.unit_price saat reservasi (sama mekanismenya dgn LandStockReservation.UnitPriceSnapshot)' AFTER land_quantity_m2,
    ADD CONSTRAINT fk_bookings_land_stock       FOREIGN KEY (land_stock_id)       REFERENCES land_stock (id),
    ADD CONSTRAINT fk_bookings_land_reservation FOREIGN KEY (land_reservation_id) REFERENCES land_stock_reservations (id),
    ADD CONSTRAINT chk_bookings_land_component CHECK (
        (land_stock_id IS NULL AND land_reservation_id IS NULL AND land_quantity_m2 IS NULL AND land_unit_price_snapshot IS NULL)
     OR (land_stock_id IS NOT NULL AND land_reservation_id IS NOT NULL AND land_quantity_m2 > 0 AND land_unit_price_snapshot >= 0)
    );

ALTER TABLE sale_contracts
    ADD COLUMN land_stock_id             BIGINT UNSIGNED NULL COMMENT 'Kelebihan Tanah proyek ini, bila kontrak menyertakan komponen tanah (dibawa dari Booking atau diisi langsung saat kontrak dibuat tanpa Booking)' AFTER unit_id,
    ADD COLUMN land_reservation_id       BIGINT UNSIGNED NULL COMMENT 'reservasi land_stock_reservations aktif untuk komponen ini, dikonversi (status=converted) saat Akad' AFTER land_stock_id,
    ADD COLUMN land_quantity_m2          DECIMAL(20,4)   NULL COMMENT 'm2 komponen tanah kontrak ini' AFTER land_reservation_id,
    ADD COLUMN land_unit_price_snapshot  DECIMAL(20,4)   NULL COMMENT 'harga/m2 dikunci saat kontrak dibuat (tidak berubah meski land_stock.unit_price admin diubah kemudian)' AFTER land_quantity_m2,
    ADD CONSTRAINT fk_sale_contracts_land_stock       FOREIGN KEY (land_stock_id)       REFERENCES land_stock (id),
    ADD CONSTRAINT fk_sale_contracts_land_reservation FOREIGN KEY (land_reservation_id) REFERENCES land_stock_reservations (id),
    ADD CONSTRAINT chk_sale_contracts_land_component CHECK (
        (land_stock_id IS NULL AND land_reservation_id IS NULL AND land_quantity_m2 IS NULL AND land_unit_price_snapshot IS NULL)
     OR (land_stock_id IS NOT NULL AND land_reservation_id IS NOT NULL AND land_quantity_m2 > 0 AND land_unit_price_snapshot >= 0)
    );
