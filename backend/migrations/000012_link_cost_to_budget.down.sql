ALTER TABLE cost_entries
    DROP FOREIGN KEY fk_cost_entries_budget_item,
    DROP INDEX idx_cost_entries_budget_item,
    DROP COLUMN budget_item_id;
