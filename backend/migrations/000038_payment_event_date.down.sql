-- Rollback hardening event_date (tidak menyentuh data lain).
ALTER TABLE contract_payment_events
    DROP COLUMN event_date;
