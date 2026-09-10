-- UAT bug: kontrak Unit + Kelebihan Tanah bundled menghasilkan piutang tanah
-- yang benar di GL (posting ke akun Piutang Customer 1-2000 yang sama dengan
-- rumah — Neraca sudah benar) TAPI tidak punya baris di sub-ledger
-- payment_schedules, sehingga engine penerimaan (ReceivePayment/planAllocation)
-- dan preview outstanding tidak pernah melihatnya sebagai piutang yang bisa
-- dialokasikan. Root cause: land_sales tidak pernah menjadi anchor di
-- payment_schedules.
--
-- Fix: land_sale_id menautkan SATU baris payment_schedules ke land_sales-nya
-- (logical FK, mengikuti konvensi payment_allocations/FE-2 — tidak ada FK fisik
-- lintas modul internal/sale ↔ internal/land). Dibuat atomik bersama
-- land.RecordAkadTx di internal/sale/repository.go Execute(), sehingga
-- otomatis ikut waterfall (planAllocation), sub-ledger alokasi, dan
-- supersede-on-cancellation yang sudah ada — TIDAK ADA payment engine kedua.
ALTER TABLE payment_schedules
    ADD COLUMN land_sale_id BIGINT UNSIGNED NULL COMMENT 'Logical FK ke land_sales.id — NULL utk cicilan unit biasa' AFTER unit_id;

CREATE INDEX idx_payment_schedules_land_sale_id ON payment_schedules (land_sale_id);
