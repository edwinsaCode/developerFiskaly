-- Rollback snapshot identity + config-version ref.
ALTER TABLE allocation_snapshot_lines
    DROP INDEX idx_alloc_snap_line_unit,
    DROP COLUMN unit_name_snapshot,
    DROP COLUMN unit_id;

ALTER TABLE allocation_snapshots
    DROP INDEX idx_alloc_snap_config_ver,
    DROP COLUMN allocation_config_version,
    DROP COLUMN allocation_config_version_id;
