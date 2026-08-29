-- Rollback FE-3 · P1. Menghapus fitur credit application; baris credit_application
-- di sub-ledger dibuang lebih dulu agar termin_payment_id bisa kembali NOT NULL.
DROP TABLE IF EXISTS credit_applications;

DELETE FROM payment_allocations WHERE allocation_type = 'credit_application';

ALTER TABLE payment_allocations
    DROP INDEX idx_pa_credit_app,
    DROP COLUMN credit_application_id,
    MODIFY COLUMN termin_payment_id BIGINT UNSIGNED NOT NULL;
