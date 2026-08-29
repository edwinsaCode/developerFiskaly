-- Increment 4 — Tax Rule konfiguratif (blueprint erp-blueprint-review.md §7).
-- ADD-only. tax_rates DIPROMOSIKAN menjadi Tax Rule penuh: applies_to (kategori
-- proyek), akun jurnal Dr/Cr konfigurable, trigger_event, calc_base,
-- effective_to, is_active. Tarif TIDAK pernah di kode — regulasi berubah =
-- tambah baris rule dengan effective_from baru.

-- ── 1. Project.tax_category — PENENTU tarif (blueprint §7) ─────────────────────
-- Catatan: BERBEDA dari customers.segment (dimensi analitik) — jangan dicampur.
ALTER TABLE projects
    ADD COLUMN tax_category VARCHAR(20) NOT NULL DEFAULT 'komersial'
        COMMENT 'subsidi|komersial — penentu tarif pajak (PPh Final 1% vs 2,5%)',
    ADD CONSTRAINT chk_prj_tax_category CHECK (tax_category IN ('subsidi', 'komersial'));

-- ── 2. tax_rates → Tax Rule penuh ──────────────────────────────────────────────
ALTER TABLE tax_rates
    ADD COLUMN name VARCHAR(200) NOT NULL DEFAULT '' AFTER rate_code,
    ADD COLUMN applies_to VARCHAR(20) NOT NULL DEFAULT 'all'
        COMMENT 'all|subsidi|komersial — dicocokkan dengan projects.tax_category (specific menang atas all)'
        AFTER rate,
    ADD COLUMN trigger_event VARCHAR(20) NOT NULL DEFAULT 'bast'
        COMMENT 'bast|invoice|payment — event pemicu (seam; saat ini bast)',
    ADD COLUMN calc_base VARCHAR(30) NOT NULL DEFAULT 'transfer_value'
        COMMENT 'basis perhitungan (seam; saat ini nilai pengalihan bruto)',
    ADD COLUMN debit_account VARCHAR(20) NOT NULL DEFAULT ''
        COMMENT 'akun Dr jurnal akrual (kosong = default legacy 5-2000)',
    ADD COLUMN credit_account VARCHAR(20) NOT NULL DEFAULT ''
        COMMENT 'akun Cr jurnal akrual (kosong = default legacy 2-4000)',
    ADD COLUMN effective_to DATETIME(3) NULL
        COMMENT 'akhir masa berlaku (NULL = terbuka)',
    ADD COLUMN is_active TINYINT(1) NOT NULL DEFAULT 1,
    ADD CONSTRAINT chk_tr_applies_to CHECK (applies_to IN ('all', 'subsidi', 'komersial'));

-- Backfill baris PPh existing: akun default + nama.
UPDATE tax_rates
SET debit_account = '5-2000', credit_account = '2-4000',
    name = 'PPh Final Pengalihan (default)'
WHERE rate_code = 'pph_final_pengalihan' AND debit_account = '';

-- ── 3. Seed rule per kategori untuk tenant existing ────────────────────────────
-- (Tenant baru: tax.SeedDefaultRates diperbarui — sumber angka yang sama.)
INSERT INTO tax_rates (tenant_id, rate_code, name, rate, applies_to, trigger_event, calc_base,
                       debit_account, credit_account, effective_from, description)
SELECT t.id, 'pph_final_pengalihan', s.name, s.rate, s.applies_to, 'bast', 'transfer_value',
       '5-2000', '2-4000', '2016-05-08 00:00:00.000', s.descr
FROM tenants t
JOIN (
    SELECT 'PPh Final Properti Subsidi' AS name, 0.010000 AS rate, 'subsidi' AS applies_to,
           'PP 34/2016: 1% atas pengalihan RS/RSS oleh developer' AS descr
    UNION ALL
    SELECT 'PPh Final Properti Komersial', 0.025000, 'komersial',
           'PP 34/2016: 2,5% atas pengalihan umum'
) s
WHERE NOT EXISTS (
    SELECT 1 FROM tax_rates x
    WHERE x.tenant_id = t.id AND x.rate_code = 'pph_final_pengalihan' AND x.applies_to = s.applies_to
);

-- ── 4. Provenance rule pada obligation (audit: rule mana yang dipakai) ─────────
ALTER TABLE tax_obligations
    ADD COLUMN tax_rule_id BIGINT UNSIGNED NULL
        COMMENT 'rule tax_rates yang dipakai saat akrual (NULL = akrual legacy)',
    ADD COLUMN applies_to VARCHAR(20) NOT NULL DEFAULT ''
        COMMENT 'snapshot applies_to rule saat akrual';
