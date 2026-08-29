-- GAP-1 fix: catat akun kredit setiap penerimaan termin secara eksplisit.
-- "2-2000" (Uang Muka, sebelum BAST) atau "1-2000" (Piutang, setelah BAST).
-- Nullable: baris lama tidak diubah (sumber kebenaran tetap baris jurnal).
ALTER TABLE termin_payments
    ADD COLUMN credit_account_code VARCHAR(20) NULL AFTER journal_entry_id;
