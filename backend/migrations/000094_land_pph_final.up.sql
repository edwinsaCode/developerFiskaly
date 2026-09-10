-- Fix remaining risk: PPh Final Pengalihan untuk Kelebihan Tanah (land_sales)
-- kini otomatis diakrual DALAM transaksi Akad (internal/land RecordAkadTx),
-- pola identik unit/BAST (AccruePPhFinalInTx) — bukan tax engine kedua,
-- melainkan tax.ResolveAccrualPlan yang sama dipanggil dari land.

-- 1. land_sales: jurnal PPh Final akrual + pembalik (mirror revenue/cogs).
ALTER TABLE land_sales
    ADD COLUMN pph_journal_id          BIGINT UNSIGNED NULL
        COMMENT 'ID jurnal akrual PPh Final Pengalihan (Event 5a) — NULL = tidak ada resolver PPh terpasang saat Akad' AFTER cogs_reversal_journal_id,
    ADD COLUMN pph_reversal_journal_id BIGINT UNSIGNED NULL
        COMMENT 'ID jurnal pembalik PPh Final saat pembatalan' AFTER pph_journal_id,
    ADD KEY idx_land_sales_pph_jrn          (pph_journal_id),
    ADD KEY idx_land_sales_pph_reversal_jrn (pph_reversal_journal_id);

-- 2. tax_obligations: land_sale_id, mutually exclusive dengan unit_id — satu
--    obligation, satu sumber. UNIQUE(tenant_id, land_sale_id) mencegah
--    double-accrual pada level DB (defense-in-depth; proteksi utama sudah
--    struktural — satu land_sale baru = satu obligation baru, tidak ada aksi
--    "re-accrue" terpisah). MySQL mengizinkan banyak NULL lewat UNIQUE index,
--    jadi baris obligation unit-linked lama (land_sale_id NULL) tidak terdampak.
ALTER TABLE tax_obligations
    ADD COLUMN land_sale_id BIGINT UNSIGNED NULL
        COMMENT 'sumber land_sales (Kelebihan Tanah) — mutually exclusive dgn unit_id' AFTER unit_id,
    ADD UNIQUE KEY uq_tax_obligations_land_sale (tenant_id, land_sale_id);
