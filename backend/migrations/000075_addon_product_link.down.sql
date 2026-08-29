-- Turun W-13. Aman selama belum ada pengakuan pendapatan addon yang diposting:
-- yang hilang hanyalah TAUTAN ke master dan snapshot akun pendapatan, bukan
-- jurnal. Setelah ada jurnal pengakuan addon, jangan turunkan — pembalikannya
-- kehilangan alamat akun yang dulu dikredit.
DROP INDEX idx_charge_items_product ON charge_items;

ALTER TABLE charge_items
    DROP COLUMN revenue_account_code,
    DROP COLUMN product_code;
