-- COA-driven payment account selection: kategori akun (cash|bank|other_asset).
-- Hanya aset yang berkategori bermakna; non-aset tetap '' (kosong).
ALTER TABLE accounts
    ADD COLUMN category VARCHAR(20) NOT NULL DEFAULT '' AFTER is_active;

-- Backfill akun bawaan berdasarkan kode (tidak mengubah data lain).
UPDATE accounts SET category = 'cash' WHERE code IN ('1-1100', '1-1200');
UPDATE accounts SET category = 'bank' WHERE code IN ('1-1300', '1-1400', '1-1500');
-- Aset selain kas/bank → other_asset (mis. Piutang, Persediaan, Aset Tetap).
UPDATE accounts SET category = 'other_asset' WHERE type = 'asset' AND category = '';

CREATE INDEX idx_accounts_category ON accounts (tenant_id, type, category);
