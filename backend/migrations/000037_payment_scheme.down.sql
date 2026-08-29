-- Rollback Increment 3 — Payment Scheme.
-- Jurnal yang pernah diposting (reklas akad, pencairan) TIDAK dihapus
-- (append-only, Invariant #5) — rollback hanya melepas metadata scheme.

-- Lepas kolom seam di sale_contracts (urutan: FK dulu).
ALTER TABLE sale_contracts
    DROP FOREIGN KEY fk_sc_scheme,
    DROP FOREIGN KEY fk_sc_finsource;
ALTER TABLE sale_contracts
    DROP INDEX idx_sc_scheme,
    DROP INDEX idx_sc_finsource,
    DROP COLUMN payment_scheme_id,
    DROP COLUMN financing_source_id,
    DROP COLUMN scheme_state,
    DROP COLUMN scheme_params_snapshot,
    DROP COLUMN approval_request_id;

ALTER TABLE termin_payments
    DROP INDEX idx_tp_finsource,
    DROP COLUMN financing_source_id;

ALTER TABLE payment_schedules
    DROP COLUMN schedule_version;

DROP TABLE IF EXISTS contract_payment_events;
DROP TABLE IF EXISTS financing_sources;
DROP TABLE IF EXISTS payment_schemes;

-- Akun 1-2200 dihapus HANYA bila belum pernah dipakai jurnal (kalau sudah,
-- dibiarkan — histori ledger tidak boleh rusak).
DELETE a FROM accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
WHERE a.code = '1-2200' AND jl.id IS NULL;
