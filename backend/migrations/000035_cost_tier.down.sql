-- Rollback Increment 2 — Cost 3-tier.
--
-- PERHATIAN (dev-only rollback): baris overhead Tenant-level (project_id IS NULL)
-- harus dihapus sebelum project_id dikembalikan menjadi NOT NULL. Jurnal beban
-- yang pernah diposting TIDAK dihapus (ledger append-only — Invariant #5);
-- rollback ini hanya melepas metadata cost_entries, bukan histori akuntansi.

DELETE FROM cost_entries WHERE project_id IS NULL;

-- MySQL menolak MODIFY NULL→NOT NULL pada kolom ber-FK (Error 1832):
-- lepas FK dulu, kembalikan NOT NULL, lalu pasang FK lagi.
ALTER TABLE cost_entries
    DROP FOREIGN KEY fk_ce_project;

ALTER TABLE cost_entries
    MODIFY COLUMN project_id BIGINT UNSIGNED NOT NULL;

ALTER TABLE cost_entries
    ADD CONSTRAINT fk_ce_project FOREIGN KEY (project_id)
        REFERENCES projects (id);

ALTER TABLE cost_entries
    DROP INDEX idx_ce_tenant_tier,
    DROP COLUMN cost_tier;
