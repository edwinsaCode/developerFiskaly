-- UAT Batch 2 §1 — Kwitansi Booking TERPISAH dari kwitansi pembayaran rumah.
-- Booking = Pendapatan Booking (di luar harga unit) → dokumen bukti terimanya
-- punya tipe & penomoran sendiri (KWB/{yyyy}/{seq}); kwitansi rumah tetap KWT.

ALTER TABLE receipts
    ADD COLUMN receipt_type VARCHAR(20) NOT NULL DEFAULT 'house_payment';
-- Histori otomatis 'house_payment'; tandai baris kwitansi booking fee lama
-- (derivable dari termin sumbernya — bukan rewrite jurnal, hanya label dokumen).
UPDATE receipts r
JOIN termin_payments tp ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
SET r.receipt_type = 'booking'
WHERE tp.payment_source = 'booking_fee';

CREATE INDEX idx_receipts_type ON receipts (tenant_id, receipt_type);

-- Penomoran per tipe dokumen: PK (tenant_id, doc_type).
ALTER TABLE receipt_sequences
    ADD COLUMN doc_type VARCHAR(20) NOT NULL DEFAULT 'house_payment';
ALTER TABLE receipt_sequences DROP PRIMARY KEY,
    ADD PRIMARY KEY (tenant_id, doc_type);
