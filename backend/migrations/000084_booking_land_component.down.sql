ALTER TABLE sale_contracts
    DROP CONSTRAINT chk_sale_contracts_land_component,
    DROP FOREIGN KEY fk_sale_contracts_land_stock,
    DROP FOREIGN KEY fk_sale_contracts_land_reservation,
    DROP COLUMN land_stock_id,
    DROP COLUMN land_reservation_id,
    DROP COLUMN land_quantity_m2,
    DROP COLUMN land_unit_price_snapshot;

ALTER TABLE bookings
    DROP CONSTRAINT chk_bookings_land_component,
    DROP FOREIGN KEY fk_bookings_land_stock,
    DROP FOREIGN KEY fk_bookings_land_reservation,
    DROP COLUMN land_stock_id,
    DROP COLUMN land_reservation_id,
    DROP COLUMN land_quantity_m2,
    DROP COLUMN land_unit_price_snapshot;
