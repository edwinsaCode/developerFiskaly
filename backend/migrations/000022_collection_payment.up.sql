-- Collection payment flow: audit trail + idempotency + partial payment tracking.

-- Audit "siapa input" + idempotency (duplicate submit protection).
ALTER TABLE termin_payments
    ADD COLUMN created_by      BIGINT UNSIGNED NULL AFTER credit_account_code,
    ADD COLUMN idempotency_key VARCHAR(64)     NULL AFTER created_by,
    ADD UNIQUE KEY uk_termin_idempotency (tenant_id, idempotency_key);

-- Partial payment: jumlah yang sudah dibayar per cicilan (cache turunan dari
-- alokasi pembayaran). 0 = belum dibayar; >= amount = lunas.
ALTER TABLE payment_schedules
    ADD COLUMN paid_amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000' AFTER amount;
