-- Rollback 000066.
--
-- Master jenis dokumen kas TIDAK dihapus bila sudah pernah menerbitkan nomor:
-- `documents` bersifat append-only, dan menghapus jenisnya membuat nomor yang
-- sudah dicetak kehilangan konfigurasi asalnya. Yang dihapus hanya jenis yang
-- belum dipakai sama sekali.

ALTER TABLE documents DROP INDEX idx_doc_reverses;
ALTER TABLE documents DROP COLUMN reverses_document_id;

ALTER TABLE journal_entries DROP INDEX uk_je_document;
ALTER TABLE journal_entries DROP COLUMN document_id;

DELETE dt FROM document_types dt
 WHERE dt.code IN ('kpr_disbursement', 'cash_in', 'cash_out',
                   'third_party_payout', 'customer_refund', 'journal_reversal')
   AND NOT EXISTS (
       SELECT 1 FROM documents d
        WHERE d.tenant_id = dt.tenant_id AND d.document_type_code = dt.code
   );
