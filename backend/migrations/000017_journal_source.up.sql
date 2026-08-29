ALTER TABLE journal_entries
  ADD COLUMN source     VARCHAR(20) NOT NULL DEFAULT 'system',
  ADD COLUMN created_by BIGINT UNSIGNED NULL;

CREATE INDEX idx_journal_entries_source ON journal_entries(source);
