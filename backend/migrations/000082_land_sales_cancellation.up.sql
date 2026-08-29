-- LT-6 (kelebihan-tanah-final-architecture §F.3) — pembatalan pasca-Akad
-- land_sales. Meniru pola sale_records.cancelled_at (perluasan minimal,
-- additive-only) — BUKAN reuse tabel `cancellations` (internal/cancellation):
-- workflow itu unit_id NOT NULL + request/approve/process + PPh/refund
-- khusus unit properti, sedangkan §F.3 mendeskripsikan land_sales sebagai
-- satu aksi atomik tanpa approval/PPh/refund terpisah (Event 3 land Dr
-- langsung Kas/Bank — reversal-nya SUDAH menjadi pengeluaran kas ke buyer,
-- tanpa mekanisme Uang Muka/Hutang Refund seperti unit).

ALTER TABLE land_sales
    ADD COLUMN cancelled_at                DATETIME(3)     NULL COMMENT 'terisi saat status=cancelled' AFTER status,
    ADD COLUMN cancel_reason               VARCHAR(500)    NULL AFTER cancelled_at,
    ADD COLUMN cancelled_by                BIGINT UNSIGNED NULL AFTER cancel_reason,
    ADD COLUMN revenue_reversal_journal_id BIGINT UNSIGNED NULL COMMENT 'ID jurnal pembalik Event 3 (ledger.Reverse)' AFTER cogs_journal_id,
    ADD COLUMN cogs_reversal_journal_id    BIGINT UNSIGNED NULL COMMENT 'ID jurnal pembalik Event 4 (mirror manual)' AFTER revenue_reversal_journal_id,
    ADD KEY idx_land_sales_rev_reversal_jrn  (revenue_reversal_journal_id),
    ADD KEY idx_land_sales_cogs_reversal_jrn (cogs_reversal_journal_id);
