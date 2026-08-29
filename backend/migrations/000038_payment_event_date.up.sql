-- Hardening Increment 3 (pasca-review): tanggal KEJADIAN BISNIS pada Payment
-- Event, terpisah dari created_at (tanggal input). Audit & laporan memakai
-- event_date (mis. akad kredit terjadi kemarin, dicatat hari ini).

ALTER TABLE contract_payment_events
    ADD COLUMN event_date DATETIME(3) NULL
        COMMENT 'tanggal kejadian bisnis (akad/pencairan/BAST/...); created_at = tanggal input'
        AFTER to_state;

-- Backfill baris existing: asumsi terbaik = tanggal input.
UPDATE contract_payment_events SET event_date = created_at WHERE event_date IS NULL;

ALTER TABLE contract_payment_events
    MODIFY COLUMN event_date DATETIME(3) NOT NULL
        COMMENT 'tanggal kejadian bisnis (akad/pencairan/BAST/...); created_at = tanggal input';
