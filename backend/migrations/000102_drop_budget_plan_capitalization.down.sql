ALTER TABLE budget_plans
  ADD COLUMN capitalized_hard_amount DECIMAL(20,4) NOT NULL DEFAULT '0.0000' AFTER approved_by,
  ADD COLUMN capitalized_at DATETIME(3) NULL AFTER capitalized_hard_amount;
