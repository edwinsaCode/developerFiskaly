ALTER TABLE land_sales
    DROP KEY idx_land_sales_cogs_reversal_jrn,
    DROP KEY idx_land_sales_rev_reversal_jrn,
    DROP COLUMN cogs_reversal_journal_id,
    DROP COLUMN revenue_reversal_journal_id,
    DROP COLUMN cancelled_by,
    DROP COLUMN cancel_reason,
    DROP COLUMN cancelled_at;
