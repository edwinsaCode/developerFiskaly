-- Rollback hardening CHECK Cost 3-tier (tidak menyentuh data).

ALTER TABLE cost_entries
    DROP CHECK chk_ce_tier_valid,
    DROP CHECK chk_ce_tier_project,
    DROP CHECK chk_ce_tier_unit,
    DROP CHECK chk_ce_tier_category;
