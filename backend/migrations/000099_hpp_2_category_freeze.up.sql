-- RULE KLIEN FREEZE (2026-09-04): HPP hanya 2 kelompok — Tanah, dan
-- Konstruksi/Hard Cost (Produksi/Perizinan/Sarana & Prasarana). Soft Cost,
-- Biaya Lain-lain, dan Operasional (dahulu "Pendanaan") BUKAN HPP walaupun
-- ada di RAB — diakui sebagai Beban saat realisasi benar-benar terjadi, bukan
-- dikapitalisasi ke Persediaan.
--
-- Akun beban 5-4600 "Beban Operasional" sudah dirujuk oleh
-- domain.CostCategoryOperational.ExpenseAccountCode() sejak RULE KLIEN
-- 2026-09-04 sebelumnya, tapi belum pernah diseed ke tabel accounts — celah
-- ditemukan & ditutup di sini bersamaan dengan penambahan 5-4700 untuk Soft Cost.

-- 1) Akun 5-4600 Beban Operasional untuk semua tenant existing (idempoten;
--    pola 000053 — clone atribut dari akun beban 5-4000).
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '5-4600', 'Beban Operasional', a.type, a.normal_balance, 1,
       'Akun beban untuk CostCategoryOperational (dahulu "Pendanaan"/financing). RULE KLIEN (2026-09-04): biaya operasional diakui LANGSUNG sebagai beban periode saat realisasi terjadi, tidak pernah dikapitalisasi ke Persediaan.',
       1, a.category
FROM accounts a
WHERE a.code = '5-4000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '5-4600');

-- 2) Akun 5-4700 Beban Soft Cost (Desain & Legal) untuk semua tenant existing.
INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_system, description, is_active, category)
SELECT a.tenant_id, '5-4700', 'Beban Soft Cost (Desain & Legal)', a.type, a.normal_balance, 1,
       'Akun beban untuk CostCategorySoft. RULE KLIEN FREEZE (2026-09-04): HPP hanya Tanah + Konstruksi/Hard Cost — Soft Cost (desain, legal) diakui sebagai beban periode saat realisasi terjadi, tidak pernah dikapitalisasi ke Persediaan.',
       1, a.category
FROM accounts a
WHERE a.code = '5-4000'
  AND NOT EXISTS (SELECT 1 FROM accounts b WHERE b.tenant_id = a.tenant_id AND b.code = '5-4700');

-- 3) Tandai akun Persediaan Soft Cost (1-3200) dan Biaya Pembiayaan (1-3300)
--    sebagai LEGACY/BEKU — tidak dihapus (Invariant #5 append-only, saldo
--    historis pra-reklasifikasi tetap harus terbaca benar di neraca).
UPDATE accounts SET description =
    'LEGACY/BEKU (RULE KLIEN FREEZE 2026-09-04): akun ini TIDAK DIPAKAI LAGI untuk transaksi baru — Soft Cost (desain, legal) direklasifikasi jadi beban periode (lihat 5-4700). Dipertahankan hanya agar saldo/histori Persediaan Soft Cost pra-reklasifikasi tetap terbaca benar di neraca.'
    WHERE code = '1-3200';

UPDATE accounts SET description =
    'LEGACY/BEKU (RULE KLIEN 2026-09-04): akun ini TIDAK DIPAKAI LAGI untuk transaksi baru — biaya pembiayaan direklasifikasi jadi CostCategoryOperational (beban periode, lihat 5-4600). Dipertahankan hanya agar saldo/histori pra-reklasifikasi tetap terbaca benar di neraca.'
    WHERE code = '1-3300';

-- 4) CHECK constraints di kedua tabel masih daftar literal 'financing' dan
--    belum pernah mengenal 'operational' sama sekali — celah lain dari
--    reklasifikasi Pendanaan sebelumnya yang tidak lengkap. Longgarkan dulu
--    (tambah 'operational', pertahankan kombinasi lama tetap sah — Invariant
--    #5 append-only: baris histori tidak boleh menjadi tidak-sah gara-gara
--    migrasi skema) sebelum rename data di langkah 5.
ALTER TABLE budget_items DROP CHECK chk_budget_item_category;
ALTER TABLE budget_items ADD CONSTRAINT chk_budget_item_category
    CHECK (category IN ('land', 'construction', 'soft', 'financing', 'operational', 'marketing', 'other'));

-- chk_ce_tier_category: matriks tier × kategori. RULE KLIEN FREEZE (2026-09-04)
-- mempersempit kategori kapitalisasi Go-level (CostTier.AllowsCategory) jadi
-- hanya land|hard untuk direct/shared, dan menambah soft ke himpunan overhead.
-- Constraint SCHEMA di sini sengaja dibuat LEBIH LONGGAR daripada Go-level:
-- baris historis (shared+soft, shared+financing) yang diposting SEBELUM freeze
-- ini tetap harus valid (append-only, tidak pernah rewrite tier histori) —
-- penegakan aturan BARU yang lebih ketat ada di domain.CostTier.AllowsCategory,
-- bukan di constraint ini.
ALTER TABLE cost_entries DROP CHECK chk_ce_tier_category;
ALTER TABLE cost_entries ADD CONSTRAINT chk_ce_tier_category CHECK (
    (cost_tier IN ('direct', 'shared') AND category IN ('land', 'hard', 'soft', 'financing', 'operational'))
    OR (cost_tier = 'overhead' AND category IN ('marketing', 'other', 'operational', 'soft'))
);

-- 5) Selesaikan rename data lama "financing" → "operational" yang tertinggal
--    dari reklasifikasi Pendanaan sebelumnya (RULE KLIEN 2026-09-04) — baris
--    RAB/realisasi yang literal masih memakai kategori string lama.
UPDATE budget_items SET category = 'operational' WHERE category = 'financing';
UPDATE cost_entries SET category = 'operational' WHERE category = 'financing';
UPDATE hpp_trueup_lines SET category = 'operational' WHERE category = 'financing';
