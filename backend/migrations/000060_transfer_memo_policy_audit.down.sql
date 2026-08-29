-- Rollback T-1 & R-A (2026-08-05).
DROP TABLE tenant_policy_changes;

DELETE FROM receipt_sequences WHERE doc_type = 'internal_transfer';

ALTER TABLE charge_settlements
    DROP INDEX uk_cs_memo,
    DROP COLUMN memo_number;
