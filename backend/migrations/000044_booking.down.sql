-- Rollback Increment 7 — Booking.
-- Akun 2-2100 TIDAK dihapus bila sudah punya jurnal (append-only ledger);
-- dinonaktifkan saja. 4-2000 dikembalikan non-aktif hanya bila tak ada jurnal.

DROP TABLE IF EXISTS bookings;

-- Nonaktifkan akun titipan yang tak pernah dipakai; yang pernah dipakai tetap
-- (histori jurnal menunjuk ke sana — Invariant #5).
UPDATE accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
SET a.is_active = 0
WHERE a.code = '2-2100' AND jl.id IS NULL;
