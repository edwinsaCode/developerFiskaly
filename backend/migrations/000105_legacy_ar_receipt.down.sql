DELETE FROM document_types WHERE code = 'legacy_ar';

ALTER TABLE receipts
    DROP INDEX uk_receipts_legacy_payment,
    DROP COLUMN legacy_receivable_payment_id;

-- termin_payment_id/unit_id DIBIARKAN nullable saat rollback: mengembalikan ke
-- NOT NULL akan gagal begitu ada satu saja kwitansi legacy_ar tersimpan (NULL
-- di kedua kolom itu, karena tidak pernah punya termin/unit). Skema tanpa data
-- boleh nullable; itu tidak melanggar invariant manapun.
