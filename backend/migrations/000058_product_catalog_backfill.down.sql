-- Rollback backfill katalog produk.
--
-- Hanya menghapus baris hasil backfill: kode yang berasal dari units.unit_type
-- DAN bukan bagian dari seed baku migrasi 000057. Katalog yang dibuat/diedit
-- manual oleh owner (kode di luar daftar unit_type) tetap aman.
--
-- Catatan jujur: bila setelah backfill owner mengedit kategori/akun salah satu
-- baris hasil backfill, rollback ini tetap menghapusnya (tidak ada penanda asal
-- baris). Down migration dipakai untuk lingkungan pengembangan.

DELETE p FROM product_types p
WHERE p.code NOT IN ('rumah','ruko','kavling','kelebihan_tanah','pdam')
  AND EXISTS (
      SELECT 1 FROM units u
      WHERE u.tenant_id = p.tenant_id AND u.unit_type = p.code
  );
