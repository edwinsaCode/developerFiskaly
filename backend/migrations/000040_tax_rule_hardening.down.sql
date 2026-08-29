-- Rollback hardening Tax Rule (tidak menyentuh data lain).

ALTER TABLE tax_obligations
    DROP COLUMN tax_rule_revision;

ALTER TABLE tax_rates
    DROP CHECK chk_tr_trigger_event;
ALTER TABLE tax_rates
    DROP CHECK chk_tr_formula;
ALTER TABLE tax_rates
    DROP COLUMN revision,
    DROP COLUMN formula;
