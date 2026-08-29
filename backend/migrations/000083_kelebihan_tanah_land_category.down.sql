-- Rollback LT-8: kembalikan master data kelebihan_tanah ke non_property/4-2000,
-- lalu persempit kembali CHECK constraint ke bentuk semula 000057.

UPDATE product_types
SET category = 'non_property', revenue_account_code = '4-2000'
WHERE code = 'kelebihan_tanah'
  AND category = 'land'
  AND revenue_account_code = '4-1100';

ALTER TABLE product_types
    DROP CHECK chk_product_category,
    ADD CONSTRAINT chk_product_category CHECK (category IN ('property', 'non_property'));
