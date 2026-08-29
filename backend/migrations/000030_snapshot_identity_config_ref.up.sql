-- P0-4 (Req #1 / D1) — Snapshot identity historis + pin config-version.
--
-- Snapshot = sumber kebenaran HISTORIS. Setelah BAST, master data (units,
-- allocation_configs) bisa berubah; snapshot harus tetap terbaca auditor tanpa
-- join ke master. Karena itu:
--   allocation_snapshots       += allocation_config_version_id (+ nomor version)
--                                 → pin basis version yang dipakai saat BAST (D1)
--   allocation_snapshot_lines  += unit_id, unit_name_snapshot
--                                 → identitas unit dibekukan di level baris
--
-- ADDITIVE (IMPL-2): hanya ADD kolom; kolom & baris lama tidak diubah. Kolom
-- nullable / DEFAULT agar snapshot P0-3 lama (bila ada di dev) tetap valid.

ALTER TABLE allocation_snapshots
    ADD COLUMN allocation_config_version_id BIGINT UNSIGNED NULL AFTER basis,
    ADD COLUMN allocation_config_version    INT             NULL AFTER allocation_config_version_id,
    ADD INDEX idx_alloc_snap_config_ver (tenant_id, allocation_config_version_id);

ALTER TABLE allocation_snapshot_lines
    ADD COLUMN unit_id            BIGINT UNSIGNED NOT NULL DEFAULT 0  AFTER snapshot_id,
    ADD COLUMN unit_name_snapshot VARCHAR(100)    NOT NULL DEFAULT '' AFTER unit_id,
    ADD INDEX idx_alloc_snap_line_unit (tenant_id, unit_id);
