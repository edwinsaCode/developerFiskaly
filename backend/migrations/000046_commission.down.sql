-- Rollback Increment 9 — Commission Engine.

DROP TABLE IF EXISTS commissions;
DROP TABLE IF EXISTS commission_rules;

UPDATE accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
SET a.is_active = 0
WHERE a.code = '2-6200' AND jl.id IS NULL;
