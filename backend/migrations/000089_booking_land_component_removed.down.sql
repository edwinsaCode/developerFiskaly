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
