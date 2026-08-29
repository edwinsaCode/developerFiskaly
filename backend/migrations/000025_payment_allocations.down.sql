-- Rollback FE-2 · P1. Bersih: payment_schedules.paid_amount sudah ada sejak
-- migrasi 000022 (mendahului FE-2), jadi tidak ada kolom lain yang disentuh.
DROP TABLE IF EXISTS payment_allocations;
