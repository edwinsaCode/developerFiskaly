-- Rollback 000063.
--
-- Catatan: turun ke skema lama HANYA aman selama belum ada grup yang benar-benar
-- memuat dua akun titipan berbeda. Setelah itu, snapshot per item dan rincian
-- settlement per akun adalah satu-satunya tempat informasi itu hidup, dan
-- menjatuhkannya berarti kehilangan kemampuan merekonstruksi jurnal — bukan
-- sesuatu yang bisa diperbaiki dari data yang tersisa.

ALTER TABLE invoices
    DROP INDEX uk_invoices_charge_group_live,
    DROP COLUMN charge_group_live;

DROP TABLE IF EXISTS charge_settlement_lines;

ALTER TABLE charge_items
    DROP INDEX idx_ci_deposit_account,
    DROP COLUMN deposit_account_code;
