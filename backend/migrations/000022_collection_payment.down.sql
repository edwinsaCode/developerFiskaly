ALTER TABLE payment_schedules DROP COLUMN paid_amount;

ALTER TABLE termin_payments
    DROP INDEX uk_termin_idempotency,
    DROP COLUMN idempotency_key,
    DROP COLUMN created_by;
