-- Rule klien UAT #3: SUBSIDI dan KOMERSIAL adalah klasifikasi PRODUK, bukan
-- sekadar skema KPR — tarif PPh Final (1% vs 2,5%) dan perlakuan akuntansi
-- lain harus bisa mengikuti PRODUK unit itu sendiri, bukan hanya
-- projects.tax_category (satu proyek boleh menjual rumah subsidi DAN
-- komersial sekaligus).
--
-- Additive murni: kolom baru NULLable. NULL = produk ini tidak menentukan
-- sendiri (jalur legacy TETAP berlaku — projects.tax_category tetap dipakai).
-- Baris "rumah" existing SENGAJA TIDAK diisi supaya tenant lama yang masih
-- memakainya terus berperilaku identik dengan sebelum migration ini.
ALTER TABLE product_types
    ADD COLUMN tax_category VARCHAR(20) NULL
        COMMENT 'subsidi|komersial — NULL = ikut projects.tax_category (legacy)'
        AFTER revenue_account_code,
    ADD CONSTRAINT chk_product_types_tax_category
        CHECK (tax_category IS NULL OR tax_category IN ('subsidi', 'komersial'));

-- Seed dua produk baru per tenant existing (idempoten) — "rumah_subsidi" dan
-- "rumah_komersial" — sebagai klasifikasi eksplisit yang bisa dipilih saat
-- membuat unit baru. Kode "rumah" lama tetap ada, tidak dihapus/dinonaktifkan
-- (data historis tidak disentuh — Invariant #5 ledger append-only berlaku
-- juga secara analog ke master data yang sudah dipakai transaksi).
INSERT INTO product_types (tenant_id, code, name, category, revenue_account_code, tax_category, is_active)
SELECT t.id, s.code, s.name, s.category, s.acct, s.tax_cat, TRUE
FROM tenants t
JOIN (
    SELECT 'rumah_subsidi'   AS code, 'Rumah Subsidi'   AS name, 'property' AS category, '4-1000' AS acct, 'subsidi'   AS tax_cat
    UNION ALL SELECT 'rumah_komersial', 'Rumah Komersial', 'property', '4-1000', 'komersial'
) s
WHERE NOT EXISTS (
    SELECT 1 FROM product_types p WHERE p.tenant_id = t.id AND p.code = s.code
);
