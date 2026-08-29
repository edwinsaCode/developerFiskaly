-- Rollback W-2 Document Domain.
--
-- `receipt_sequences` dan `invoice_sequences` sengaja TIDAK dihapus oleh
-- 000065.up, justru supaya rollback tidak kehilangan seri. Tetapi dokumen yang
-- terbit SETELAH engine baru dipakai hanya tercatat di engine baru — kalau
-- counter lama dibiarkan apa adanya, nomor yang sudah dicetak akan terpakai
-- kedua kalinya. Karena itu counter lama dikembalikan dulu, baru tabel dibuang.

UPDATE receipt_sequences rs
  JOIN (
      SELECT tenant_id, document_type_code, MAX(last_val) AS v
        FROM document_sequences
       GROUP BY tenant_id, document_type_code
  ) ds ON ds.tenant_id = rs.tenant_id AND ds.document_type_code = rs.doc_type
   SET rs.next_val = GREATEST(rs.next_val, ds.v);

UPDATE invoice_sequences is2
  JOIN (
      SELECT tenant_id, MAX(last_val) AS v
        FROM document_sequences
       WHERE document_type_code = 'invoice'
       GROUP BY tenant_id
  ) ds ON ds.tenant_id = is2.tenant_id
   SET is2.next_val = GREATEST(is2.next_val, ds.v);

DROP TABLE IF EXISTS documents;
DROP TABLE IF EXISTS document_sequences;
DROP TABLE IF EXISTS document_types;
