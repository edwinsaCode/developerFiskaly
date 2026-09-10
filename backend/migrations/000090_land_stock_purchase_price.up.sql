-- kelebihan-tanah-konversi-kontrak-2026-08: pool Kelebihan Tanah butuh dua
-- harga terpisah — Harga Beli (purchase_price, harga beli per m2 yang sudah
-- pasti diketahui saat setup pool) dan Harga Jual (unit_price, sudah ada —
-- satu-satunya harga yang dipakai di reservasi/Akad/DPP). Sebelum ini pool
-- cuma punya unit_price, sehingga admin tidak punya tempat mencatat harga
-- beli tanah kelebihan.
--
-- KOREKSI KLIEN (2026-08-31, lihat internal/land/hpp_resolver.go): purchase_price
-- DIPAKAI LANGSUNG sebagai tarif HPP land_stock (PurchasePriceLandHPPResolver,
-- resolver produksi) DAN sebagai carve-out tetapnya dari pool biaya Land
-- project-wide (internal/allocation/land_pool.go ComputeLandPool) — bukan lagi
-- murni informasional. Ini biaya AKUMULASI sungguhan (Invariant #4): tanah
-- kelebihan dibeli terpisah dengan harga per-m2 yang sudah pasti, bukan
-- estimasi/alokasi dari RAB/actual project-wide.
ALTER TABLE land_stock
    ADD COLUMN purchase_price DECIMAL(20,4) NOT NULL DEFAULT '0.0000' COMMENT 'Harga beli per m2 land_stock — dipakai langsung sbg HPP & carve-out pool Land' AFTER unit_price;
