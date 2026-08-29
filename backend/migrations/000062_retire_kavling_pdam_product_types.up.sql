-- W-1b — Pensiunkan `kavling` dan `pdam` dari master Produk Penjualan.
--
-- Keputusan bisnis FINAL klien 2026-08-06:
--   * Produk yang DIJUAL hanya: Rumah, Ruko, Kelebihan Tanah. "Tidak ada Kavling."
--   * PDAM bukan produk — PDAM adalah JENIS BIAYA REALISASI (titipan), dan sejak
--     migration 000061 ia hidup di `realization_charge_types`.
--
-- NONAKTIFKAN, BUKAN HAPUS. Baris master yang pernah dipakai adalah bagian dari
-- jejak audit: unit lama, jurnal lama, dan laporan lama menunjuk ke kode ini.
-- Menghapusnya membuat data historis kehilangan artinya — dan invariant #5
-- (ledger append-only) menuntut histori tetap terbaca apa adanya.
--
-- FAIL-SAFE: penonaktifan hanya berlaku untuk tenant yang TIDAK punya unit
-- ber-tipe tersebut. Kalau sebuah tenant ternyata masih memakainya, barisnya
-- SENGAJA dibiarkan aktif — lebih baik satu tenant tertinggal dan terlihat di
-- audit daripada unit produksi kehilangan resolusi product policy (yang
-- fail-closed, sehingga seluruh transaksi unit itu akan tertolak).

-- ── 1. Jejak audit DULU (TD-8) ──────────────────────────────────────────────
-- Ditulis sebelum UPDATE supaya yang tercatat persis baris yang benar-benar
-- berubah — bukan baris yang kebetulan sudah nonaktif sejak sebelumnya.
-- changed_by NULL = perubahan sistem (migrasi), bukan tindakan seorang admin.
INSERT INTO master_data_changes (tenant_id, entity, entity_code, field, old_value, new_value, changed_by)
SELECT pt.tenant_id, 'product_type', pt.code, 'is_active', 'true', 'false', NULL
FROM product_types pt
WHERE pt.code IN ('kavling', 'pdam')
  AND pt.is_active = 1
  AND NOT EXISTS (
      SELECT 1 FROM units u
      WHERE u.tenant_id = pt.tenant_id
        AND u.unit_type = pt.code
  );

-- ── 2. Baru nonaktifkan ─────────────────────────────────────────────────────
UPDATE product_types pt
SET pt.is_active = 0
WHERE pt.code IN ('kavling', 'pdam')
  AND pt.is_active = 1
  AND NOT EXISTS (
      SELECT 1 FROM units u
      WHERE u.tenant_id = pt.tenant_id
        AND u.unit_type = pt.code
  );
