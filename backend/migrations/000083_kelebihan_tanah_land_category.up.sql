-- LT-8 (docs/kelebihan-tanah-final-architecture-2026-08.md §G) — bekukan
-- jalur addon lama (W-13, charge_group kind='addon') untuk Kelebihan Tanah.
--
-- internal/domain/product_category.go (LT-1) sudah punya ProductCategoryLand
-- sejak awal, dengan ParticipatesInHPP()==true — tapi belum ada peserta yang
-- memakainya. internal/charge/addon.go's resolveAddonProduct SUDAH menolak
-- produk manapun yang ParticipatesInHPP() (baris "if pol.Category.
-- ParticipatesInHPP() { return ErrProductNotAddon }") — guard fail-closed
-- generik ini ditulis untuk kategori `property` (rumah/ruko lewat addon), dan
-- otomatis berlaku untuk `land` tanpa perubahan kode apa pun begitu master
-- data product_types mengklasifikasikan kelebihan_tanah sebagai `land`.
--
-- Migrasi ini murni PEMBARUAN MASTER DATA, bukan koreksi jurnal: tidak ada
-- jurnal historis manapun yang pernah menyentuh akun lewat baris katalog ini
-- (satu-satunya transaksi addon nyata, charge_group id=11 tenant 9900245, sudah
-- posting ke 4-2000 dan TETAP di 4-2000 — histori tidak diubah; hanya baris
-- master untuk transaksi BARU yang berubah). revenue_account_code diperbarui
-- ke 4-1100 murni demi konsistensi katalog — nilainya tidak lagi pernah dibaca
-- untuk kelebihan_tanah sejak guard ParticipatesInHPP() menolak lebih dulu
-- (internal/charge/addon.go baris ~102, sebelum RevenueAccountCode dipakai).
--
-- CHECK constraint chk_product_category (migration 000057) diperlebar dulu
-- sebelum UPDATE, karena hanya mengizinkan ('property','non_property') —
-- predates LT-1's penambahan `land` ke domain layer.
ALTER TABLE product_types
    DROP CHECK chk_product_category,
    ADD CONSTRAINT chk_product_category CHECK (category IN ('property', 'non_property', 'land'));

UPDATE product_types
SET category = 'land', revenue_account_code = '4-1100'
WHERE code = 'kelebihan_tanah'
  AND category = 'non_property'
  AND revenue_account_code = '4-2000';
