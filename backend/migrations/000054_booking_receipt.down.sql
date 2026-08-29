-- Rollback kwitansi booking terpisah. Baris sequence doc_type non-default
-- dihapus agar PK tunggal valid kembali; nomor KWB yang sudah terbit tetap
-- tersimpan di receipts (dokumen append-only).
DELETE FROM receipt_sequences WHERE doc_type <> 'house_payment';
ALTER TABLE receipt_sequences DROP PRIMARY KEY,
    ADD PRIMARY KEY (tenant_id);
ALTER TABLE receipt_sequences DROP COLUMN doc_type;

DROP INDEX idx_receipts_type ON receipts;
ALTER TABLE receipts DROP COLUMN receipt_type;
