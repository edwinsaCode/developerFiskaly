DROP INDEX idx_payment_schedules_land_sale_id ON payment_schedules;

ALTER TABLE payment_schedules
    DROP COLUMN land_sale_id;
