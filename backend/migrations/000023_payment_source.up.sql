-- Unifikasi jalur penerimaan: catat ASAL pencatatan pembayaran untuk audit.
-- collection | schedule_received | unit_termin. Semua tetap lewat ReceivePayment.
ALTER TABLE termin_payments
    ADD COLUMN payment_source VARCHAR(20) NULL AFTER idempotency_key;
