-- W-13 — Kelebihan Tanah adalah PRODUK TAMBAHAN, bukan unit.
--
-- Koreksi klien: "Produk kelebihan tanah harus sinkron ke produk tambahan pas
-- mau jual unit, bukan diperlakukan sama cara jualnya seperti unit rumah."
--
-- Selama ini kelebihan tanah punya DUA jalan jual yang tidak pernah bertemu:
--
--   (a) sebagai UNIT — `units.unit_type = 'kelebihan_tanah'`. Ia lalu berdiri di
--       papan penjualan berdampingan dengan rumah, padahal kategorinya
--       non_property: tidak pernah menerima alokasi biaya, tidak pernah
--       membentuk HPP, tidak menggeser progress fisik. Sebuah "unit" yang tidak
--       bisa menjadi unit.
--
--   (b) sebagai item grup addon — label KETIK BEBAS tanpa tautan ke katalog
--       produk. Akibatnya uang customer mendarat di 2-2400 Titipan Realisasi
--       (default kolom) dan TIDAK PERNAH menjadi pendapatan: kelebihan tanah
--       terjual, kasnya masuk, laba ruginya diam.
--
-- Jalur (a) ditutup di aplikasi — unit hanya untuk produk berkategori property.
-- Migrasi ini memperbaiki jalur (b): item addon menunjuk MASTER katalog produk
-- dan membawa snapshot akun pendapatannya, persis seperti W-1 memberi item
-- realisasi snapshot akun kewajibannya. Dua master, satu pola.
--
-- Baris lama TIDAK ditulis ulang (Invariant #5). NULL = item pra-W-13; ia tetap
-- dibaca lewat `deposit_account_code` yang sudah tersimpan, sehingga jurnal
-- pembalik selalu memukul akun yang dulu benar-benar dikredit.
ALTER TABLE charge_items
    ADD COLUMN product_code VARCHAR(50) NULL AFTER charge_type_code,
    ADD COLUMN revenue_account_code VARCHAR(20) NULL AFTER deposit_account_code;

CREATE INDEX idx_charge_items_product ON charge_items (tenant_id, product_code);
