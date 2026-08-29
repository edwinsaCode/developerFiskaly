-- Increment 2 — Cost 3-tier (Direct / Shared / Overhead).
-- Blueprint: docs/blueprint-domain-additions.md §7 (CORE).
--
-- 1. cost_entries.cost_tier — klasifikasi eksplisit setiap biaya:
--      direct   → jelas milik SATU unit (unit_id WAJIB); Dr Persediaan 1-3xxx
--      shared   → dipakai banyak unit, pool alokasi (unit_id NULL); Dr Persediaan 1-3xxx
--      overhead → biaya perusahaan/period expense (unit_id NULL); Dr Beban 5-3000/5-4000,
--                 TIDAK PERNAH dikapitalisasi, TIDAK PERNAH masuk HPP.
--    Backfill deterministik (disetujui): unit_id IS NOT NULL → direct, NULL → shared.
--    (Semua baris lama adalah biaya kapitalisasi; tidak ada yang menjadi overhead.)
--
-- 2. project_id menjadi NULLABLE — KHUSUS tier overhead boleh tanpa project
--    (biaya Tenant-level, blueprint review §4). Overhead juga boleh di-tag ke
--    project sebagai cost center untuk reporting (tetap bukan HPP).
--    Tier direct/shared tetap WAJIB project (ditegakkan di service).
--
-- Additive (IMPL-2): tidak ada DROP/ubah makna kolom lama; baris lama hanya
-- mendapat nilai cost_tier hasil backfill.

ALTER TABLE cost_entries
    ADD COLUMN cost_tier VARCHAR(20) NOT NULL DEFAULT ''
        COMMENT 'direct|shared|overhead — klasifikasi 3-tier (blueprint §7)'
        AFTER category,
    ADD INDEX idx_ce_tenant_tier (tenant_id, cost_tier);

-- Backfill deterministik untuk baris existing (semuanya biaya kapitalisasi).
UPDATE cost_entries
SET cost_tier = IF(unit_id IS NULL, 'shared', 'direct')
WHERE cost_tier = '';

-- Hapus DEFAULT '' agar insert tanpa cost_tier eksplisit gagal (service selalu mengisi).
ALTER TABLE cost_entries
    MODIFY COLUMN cost_tier VARCHAR(20) NOT NULL
        COMMENT 'direct|shared|overhead — klasifikasi 3-tier (blueprint §7)';

-- Overhead Tenant-level: project_id boleh NULL (hanya untuk cost_tier=overhead).
ALTER TABLE cost_entries
    MODIFY COLUMN project_id BIGINT UNSIGNED NULL
        COMMENT 'NULL hanya untuk cost_tier=overhead (biaya Tenant-level); direct/shared wajib project';
