ALTER TABLE tax_obligations
    DROP KEY uq_tax_obligations_land_sale,
    DROP COLUMN land_sale_id;

ALTER TABLE land_sales
    DROP KEY idx_land_sales_pph_reversal_jrn,
    DROP KEY idx_land_sales_pph_jrn,
    DROP COLUMN pph_reversal_journal_id,
    DROP COLUMN pph_journal_id;
