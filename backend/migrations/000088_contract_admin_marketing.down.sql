ALTER TABLE sale_contracts
    DROP INDEX idx_sc_admin_marketing,
    DROP COLUMN admin_marketing_person_id;
