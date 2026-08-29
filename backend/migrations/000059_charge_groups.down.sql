-- Rollback Billing Batch 2 — Charge Group.
DELETE FROM accounts WHERE code = '2-2400' AND is_system = 1;

ALTER TABLE tenants DROP COLUMN require_realization_settled;

ALTER TABLE invoices
    DROP INDEX idx_invoices_charge_group,
    DROP COLUMN charge_group_id;

ALTER TABLE receipts
    DROP INDEX idx_receipts_charge_group,
    DROP COLUMN charge_group_id;

ALTER TABLE payment_allocations
    DROP INDEX uk_pa_termin_target,
    ADD UNIQUE KEY uk_pa_termin_target (tenant_id, termin_payment_id, schedule_key);

ALTER TABLE payment_allocations
    DROP INDEX idx_pa_charge_item,
    DROP COLUMN charge_item_key,
    DROP COLUMN charge_item_id;

ALTER TABLE termin_payments
    DROP INDEX idx_tp_charge_group,
    DROP COLUMN charge_group_id;

DROP TABLE charge_settlements;
DROP TABLE charge_payouts;
DROP TABLE charge_item_adjustments;
DROP TABLE charge_items;
DROP TABLE charge_groups;
