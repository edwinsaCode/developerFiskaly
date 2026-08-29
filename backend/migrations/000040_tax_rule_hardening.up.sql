-- Hardening Increment 4 (pasca-review): revision rule + typed trigger + formula seam.
--
-- 1. tax_rates.revision — identitas revisi KONFIGURASI sebuah rule. Rule bersifat
--    append-preferred (regulasi berubah = baris baru), tapi bila sebuah baris
--    diubah (mis. dinonaktifkan/dikoreksi) revision WAJIB naik. Obligation
--    membekukan (rule_id, revision) → audit eksplisit "konfigurasi versi berapa
--    yang dipakai", bukan sekadar pointer.
-- 2. CHECK trigger_event — vocabulary event perpajakan konsisten (typed enum).
-- 3. tax_rates.formula — seam perhitungan (proportional sekarang; progressive/
--    threshold/fixed_amount/exemption menyusul sebagai formula baru TANPA
--    menyentuh TaxRuleResolver).

ALTER TABLE tax_rates
    ADD COLUMN revision INT NOT NULL DEFAULT 1
        COMMENT 'revisi konfigurasi baris rule; naik pada setiap perubahan baris',
    ADD COLUMN formula VARCHAR(30) NOT NULL DEFAULT 'proportional'
        COMMENT 'formula perhitungan: proportional (rate × base); seam utk progressive/threshold/fixed_amount/exemption',
    ADD CONSTRAINT chk_tr_trigger_event CHECK (
        trigger_event IN ('bast', 'invoice', 'payment')
    ),
    ADD CONSTRAINT chk_tr_formula CHECK (
        formula IN ('proportional', 'progressive', 'threshold', 'fixed_amount', 'exemption')
    );

ALTER TABLE tax_obligations
    ADD COLUMN tax_rule_revision INT NULL
        COMMENT 'snapshot revisi rule saat akrual (bersama tax_rule_id = provenance eksplisit)';
