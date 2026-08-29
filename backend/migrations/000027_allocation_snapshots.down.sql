-- Rollback P0-2/P0-3 snapshot alokasi HPP.
ALTER TABLE sale_records
    DROP INDEX idx_sale_records_snapshot,
    DROP COLUMN budget_plan_version,
    DROP COLUMN budget_plan_id,
    DROP COLUMN allocation_snapshot_id,
    DROP COLUMN hpp_method;

DROP TABLE IF EXISTS allocation_snapshot_lines;
DROP TABLE IF EXISTS allocation_snapshots;
