-- Rollback party masters.
ALTER TABLE sale_contracts
    DROP INDEX idx_sc_salesperson,
    DROP INDEX idx_sc_customer,
    DROP COLUMN sales_person_id,
    DROP COLUMN customer_id;

DROP TABLE IF EXISTS sales_persons;
DROP TABLE IF EXISTS sales_teams;
DROP TABLE IF EXISTS customers;
