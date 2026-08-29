-- Rollback Increment 8 — Cancellation & Refund.

ALTER TABLE bookings DROP CHECK chk_bookings_disposition;
ALTER TABLE bookings DROP CHECK chk_bookings_status_disposition;
ALTER TABLE bookings
    ADD CONSTRAINT chk_bookings_disposition CHECK (
        fee_disposition IN ('held','transferred','forfeited','pending_refund')
    ),
    ADD CONSTRAINT chk_bookings_status_disposition CHECK (
        (status = 'active'    AND fee_disposition = 'held')
     OR (status = 'converted' AND fee_disposition = 'transferred')
     OR (status IN ('expired','cancelled') AND fee_disposition IN ('forfeited','pending_refund'))
    );

ALTER TABLE sale_records
    DROP COLUMN cancelled_at,
    DROP COLUMN cancellation_id;

DROP TABLE IF EXISTS refunds;
DROP TABLE IF EXISTS cancellations;

-- Akun 2-2200 yang tak pernah dipakai dinonaktifkan (yang berjurnal tetap —
-- Invariant #5).
UPDATE accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
SET a.is_active = 0
WHERE a.code = '2-2200' AND jl.id IS NULL;
