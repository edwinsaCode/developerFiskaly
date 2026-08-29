-- UAT Batch 2 §4 — Bulk Unit Generator.
-- type_label: label komersial tipe rumah (mis. "36/72") — BUKAN nilai uang,
-- murni atribut katalog; nullable (baris lama tidak disentuh).
ALTER TABLE units
    ADD COLUMN type_label VARCHAR(30) NULL;
