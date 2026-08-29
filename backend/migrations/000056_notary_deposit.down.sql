DROP TABLE IF EXISTS notary_deposits;

-- Akun 2-2300 tidak dihapus bila sudah berjurnal (append-only; pola 000044).
DELETE a FROM accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
WHERE a.code = '2-2300' AND jl.id IS NULL;
