DELETE FROM product_types WHERE code IN ('rumah_subsidi', 'rumah_komersial');

ALTER TABLE product_types
    DROP CONSTRAINT chk_product_types_tax_category,
    DROP COLUMN tax_category;
