ALTER TABLE cancellations
    DROP FOREIGN KEY fk_cancellations_land_sale,
    DROP COLUMN land_cogs_reversal_journal_id,
    DROP COLUMN land_revenue_reversal_journal_id,
    DROP COLUMN land_sale_id;
