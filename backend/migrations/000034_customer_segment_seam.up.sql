-- Increment 1 (penyempurnaan) — Customer segment seam.
--
-- Dimensi reporting BISNIS (berbeda dari `type` individual/company). Contoh nilai:
-- 'subsidi' | 'komersial' | 'investor' | 'corporate'. Nilai bisa berkembang →
-- sengaja TANPA CHECK constraint. RESERVED SEAM: belum dipakai logika apa pun,
-- disediakan agar reporting/segmentasi masa depan tidak perlu migrasi + backfill.
-- Additive (IMPL-2), nullable-equivalent (default '').

ALTER TABLE customers
    ADD COLUMN segment VARCHAR(30) NOT NULL DEFAULT '' COMMENT 'dimensi reporting bisnis: subsidi|komersial|investor|corporate (reserved)';
