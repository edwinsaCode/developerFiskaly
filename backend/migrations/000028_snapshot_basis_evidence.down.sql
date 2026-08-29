-- Rollback bukti basis pada baris snapshot.
ALTER TABLE allocation_snapshot_lines
    DROP COLUMN allocation_percentage,
    DROP COLUMN basis_value,
    DROP COLUMN basis_type;
