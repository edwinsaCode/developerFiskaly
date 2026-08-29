ALTER TABLE land_stock_reservations
    DROP FOREIGN KEY fk_land_reservations_sale;

DROP TABLE IF EXISTS land_allocations;
DROP TABLE IF EXISTS land_sales;
