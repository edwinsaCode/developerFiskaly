-- Rollback HPP 2-kategori freeze (2026-09-04).
--
-- NOTE: langkah 4 di up.sql (rename budget_items/cost_entries/hpp_trueup_lines
-- category 'financing'→'operational') TIDAK dibalik di sini — begitu baris lama
-- bergabung dengan baris 'operational' yang sudah ada sebelumnya, tidak ada cara
-- deterministik memisahkan mana yang berasal dari 'financing' (Invariant #5:
-- append-only, tidak menebak histori). Efeknya jinak: 'operational' tetap
-- kategori beban yang sah di kedua arah migrasi.

-- Kembalikan CHECK constraints ke bentuk semula (000011/000036). Ini bisa gagal
-- bila ada baris category='operational' pasca-migrasi (termasuk baris yang
-- direname up.sql langkah 5, atau baris baru yang ditulis app setelah freeze) —
-- itu sinyal yang benar: rollback skema TIDAK bisa mengembalikan baris ke
-- bentuk yang constraint lama tidak izinkan (Invariant #5, lihat catatan di atas).
ALTER TABLE budget_items DROP CHECK chk_budget_item_category;
ALTER TABLE budget_items ADD CONSTRAINT chk_budget_item_category
    CHECK (category IN ('land', 'construction', 'soft', 'financing', 'marketing', 'other'));

ALTER TABLE cost_entries DROP CHECK chk_ce_tier_category;
ALTER TABLE cost_entries ADD CONSTRAINT chk_ce_tier_category CHECK (
    (cost_tier IN ('direct', 'shared') AND category IN ('land', 'hard', 'soft', 'financing'))
    OR (cost_tier = 'overhead' AND category IN ('marketing', 'other'))
);

-- Akun 5-4600/5-4700 TIDAK dihapus bila sudah punya jurnal (append-only ledger) —
-- pola 000053.
DELETE a FROM accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
WHERE a.code = '5-4600' AND jl.id IS NULL;

DELETE a FROM accounts a
LEFT JOIN journal_lines jl ON jl.account_id = a.id
WHERE a.code = '5-4700' AND jl.id IS NULL;

UPDATE accounts SET description =
    '★ Desain, legal yang dikapitalisasi (perizinan pembangunan fisik BUKAN di sini — lihat 1-3100)'
    WHERE code = '1-3200';

UPDATE accounts SET description =
    '★ Bunga/biaya pinjaman yang dikapitalisasi'
    WHERE code = '1-3300';
