-- Rollback W-1b: aktifkan kembali kavling & pdam, dan cabut baris audit yang
-- ditulis oleh migrasi ini (yang ditandai changed_by NULL — perubahan sistem).
--
-- Hanya baris yang DIUBAH migrasi ini yang dipulihkan: dikenali dari adanya
-- baris audit pasangannya. Tenant yang sudah menonaktifkan kavling/pdam sendiri
-- lewat admin (changed_by terisi) tidak ikut dinyalakan lagi.
UPDATE product_types pt
SET pt.is_active = 1
WHERE pt.code IN ('kavling', 'pdam')
  AND pt.is_active = 0
  AND EXISTS (
      SELECT 1 FROM master_data_changes m
      WHERE m.tenant_id = pt.tenant_id
        AND m.entity = 'product_type'
        AND m.entity_code = pt.code
        AND m.field = 'is_active'
        AND m.new_value = 'false'
        AND m.changed_by IS NULL
  );

DELETE FROM master_data_changes
WHERE entity = 'product_type'
  AND entity_code IN ('kavling', 'pdam')
  AND field = 'is_active'
  AND new_value = 'false'
  AND changed_by IS NULL;
