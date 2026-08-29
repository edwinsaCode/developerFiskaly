-- Rollback rule Pendapatan Booking → kembali ke CHECK 000052 (R4 Opsi A).
-- Akun 4-2100 TIDAK dihapus bila sudah punya jurnal (append-only ledger) —
-- pola 000044.

ALTER TABLE bookings DROP CHECK chk_bookings_disposition;
ALTER TABLE bookings DROP CHECK chk_bookings_status_disposition;
ALTER TABLE bookings
    ADD CONSTRAINT chk_bookings_disposition CHECK (
        fee_disposition IN ('held','transferred','forfeited','pending_refund','refunded')
    ),
    ADD CONSTRAINT chk_bookings_status_disposition CHECK (
        (status = 'active'    AND fee_disposition = 'held')
     OR (status = 'converted' AND fee_disposition IN ('transferred','held','forfeited','pending_refund','refunded'))
     OR (status IN ('expired','cancelled') AND fee_disposition IN ('forfeited','pending_refund','refunded'))
    );

DELETE a FROM accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
WHERE a.code = '4-2100' AND jl.id IS NULL;
