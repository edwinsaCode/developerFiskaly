ALTER TABLE sale_records
  DROP COLUMN handed_over_at,
  CHANGE COLUMN recognition_date bast_date DATETIME(3) NOT NULL;
