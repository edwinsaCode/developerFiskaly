-- Product Catalog Hardening (H-1/H-2/M-1) — BACKFILL KATALOG PRODUK.
--
-- Konteks: sebelum ini `units.unit_type` yang tidak ada di master product_types
-- diperlakukan diam-diam sebagai "rumah" dengan akun pendapatan 4-1000
-- (fail-open). Data nyata memakai puluhan nilai bebas ("Villa A", "Tipe 36/72",
-- "Hook", "Perumahan Subsidi", ...), jadi HAMPIR SELURUH unit bergantung pada
-- fallback diam-diam itu.
--
-- Kode sekarang FAIL-CLOSED: unit_type yang tidak terdaftar menolak alokasi HPP
-- dan menolak BAST. Migrasi ini mengubah fallback implisit menjadi baris master
-- yang eksplisit dan bisa diaudit — TANPA mengubah satu rupiah pun:
--   category = 'property'            (perilaku lama: semua unit ikut HPP)
--   revenue_account_code = '4-1000'  (perilaku lama: akun default BAST)
-- Jurnal yang dihasilkan untuk unit-unit ini identik byte-per-byte dengan
-- sebelumnya. Setelah ini, owner bisa mengoreksi kategori/akun lewat UI
-- Pengaturan → Katalog Produk bila ada produk yang sebenarnya non-properti.
--
-- Idempoten (NOT EXISTS + unique key tenant_id+code). Perhatikan collation
-- utf8mb4_unicode_ci: GROUP BY di bawah sudah mengelompokkan case-insensitive,
-- sama dengan cara unique key dan resolver mencocokkan kode.

INSERT INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
SELECT u.tenant_id,
       MIN(u.unit_type)  AS code,
       MIN(u.unit_type)  AS name,
       'property',
       '4-1000',
       TRUE
FROM units u
WHERE TRIM(u.unit_type) <> ''
  AND NOT EXISTS (
      SELECT 1 FROM product_types p
      WHERE p.tenant_id = u.tenant_id AND p.code = u.unit_type
  )
GROUP BY u.tenant_id, u.unit_type;

-- Produk yang sudah terdaftar tapi ternonaktifkan sementara TIDAK disentuh:
-- resolver menolaknya secara sadar (produk nonaktif = keputusan owner), bukan
-- lubang data.
