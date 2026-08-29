-- Phase 8 fix: tambah kolom PKP ke sale_contracts.
-- dpp_amount       = harga dasar sebelum PPN
-- is_pkp           = apakah penjual PKP
-- vat_rate_snapshot = snapshot tarif PPN saat kontrak (histori tidak berubah bila tarif berubah)
-- gross_amount      = dpp_amount * (1 + vat_rate_snapshot) untuk PKP; == dpp_amount untuk non-PKP
--
-- TotalPrice (kolom lama) dipertahankan sebagai alias gross_amount untuk backward-compat.

ALTER TABLE sale_contracts
    ADD COLUMN dpp_amount        DECIMAL(20,4)  NOT NULL DEFAULT '0.0000'    COMMENT 'DPP / harga dasar sebelum PPN',
    ADD COLUMN is_pkp            TINYINT(1)     NOT NULL DEFAULT 0            COMMENT '1 = penjual PKP, PPN terutang',
    ADD COLUMN vat_rate_snapshot DECIMAL(10,6)  NOT NULL DEFAULT '0.000000'  COMMENT 'Snapshot tarif PPN saat kontrak, 0 untuk non-PKP',
    ADD COLUMN gross_amount      DECIMAL(20,4)  NOT NULL DEFAULT '0.0000'    COMMENT 'Tagihan bruto ke buyer: DPP+PPN (PKP) atau DPP (non-PKP)';

-- Backfill data lama: semua kontrak yang ada adalah non-PKP.
-- dpp_amount = total_price (DPP == Gross untuk non-PKP)
-- gross_amount = total_price
UPDATE sale_contracts
SET dpp_amount   = total_price,
    gross_amount = total_price
WHERE gross_amount = '0.0000';
