-- Rollback Increment 4 — Tax Rule. Obligation & jurnal yang sudah terjadi
-- TIDAK dihapus (append-only); hanya kolom konfigurasi yang dilepas.

ALTER TABLE tax_obligations
    DROP COLUMN tax_rule_id,
    DROP COLUMN applies_to;

-- Baris rule per-kategori (hasil seed §3) dihapus SEBELUM kolom applies_to
-- hilang — agar tidak menjadi duplikat generic yang membingungkan resolusi lama.
DELETE FROM tax_rates WHERE applies_to IN ('subsidi', 'komersial');

ALTER TABLE tax_rates
    DROP CHECK chk_tr_applies_to;
ALTER TABLE tax_rates
    DROP COLUMN name,
    DROP COLUMN applies_to,
    DROP COLUMN trigger_event,
    DROP COLUMN calc_base,
    DROP COLUMN debit_account,
    DROP COLUMN credit_account,
    DROP COLUMN effective_to,
    DROP COLUMN is_active;

ALTER TABLE projects
    DROP CHECK chk_prj_tax_category;
ALTER TABLE projects
    DROP COLUMN tax_category;
