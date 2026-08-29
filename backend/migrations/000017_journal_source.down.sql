DROP INDEX idx_journal_entries_source ON journal_entries;
ALTER TABLE journal_entries
  DROP COLUMN created_by,
  DROP COLUMN source;
