-- kelebihan-tanah-booking-integration-2026-08 — pembatalan unit pasca-Akad
-- yang kontraknya menyertakan komponen Kelebihan Tanah bundled (booking-embedded,
-- BUKAN land_sales standalone v1/§F.3 yang punya workflow batalnya sendiri di
-- 000082) harus ikut membalikkan jurnal pendapatan+HPP tanah tersebut — satu
-- transaksi yang sama dengan pembalikan Event 3/4 unit (land.CancelLandSaleTx,
-- dipanggil dari internal/cancellation.Process). Kolom ini murni jejak audit;
-- land_sales sendiri sudah menyimpan status+reversal journal id-nya masing-
-- masing (000082) — kolom di sini memudahkan penelusuran dari sisi cancellation
-- tanpa join, konsisten dengan pola revenue_reversal_journal_id/dst yang sudah ada.
ALTER TABLE cancellations
    ADD COLUMN land_sale_id                     BIGINT UNSIGNED NULL COMMENT 'land_sales yang ikut dibalik, bila kontrak unit ini punya komponen Kelebihan Tanah bundled' AFTER cogs_reversal_journal_id,
    ADD COLUMN land_revenue_reversal_journal_id  BIGINT UNSIGNED NULL COMMENT 'ID jurnal pembalik pendapatan Akad Kelebihan Tanah' AFTER land_sale_id,
    ADD COLUMN land_cogs_reversal_journal_id     BIGINT UNSIGNED NULL COMMENT 'ID jurnal pembalik HPP Akad Kelebihan Tanah' AFTER land_revenue_reversal_journal_id,
    ADD CONSTRAINT fk_cancellations_land_sale FOREIGN KEY (land_sale_id) REFERENCES land_sales (id);
