package billing

// W-3.3 — pemetaan jurnal kas → dokumen kwitansi untuk backfill.
//
// Tinggal di sini, bukan di `internal/document`, karena `receipts` dan
// `termin_payments` milik modul ini. Engine dokumen tidak perlu — dan tidak
// boleh — tahu tabel modul mana saja yang menerbitkan dokumen; ia hanya
// menerima seam `document.BusinessDocumentResolver`.

import (
	"fmt"

	"gorm.io/gorm"

	"esaproperti/internal/document"
)

// JournalDocumentResolver menjawab: "jurnal kas ini bukti kwitansinya mana?"
//
// Rantainya `journal_entries.id` ← `termin_payments.journal_entry_id` →
// `receipts.termin_payment_id` → `documents.source_id`. Satu kwitansi selalu
// milik satu termin, dan satu termin memposting satu jurnal kas, jadi
// pemetaannya satu-satu. Bila ternyata lebih dari satu dokumen menunjuk jurnal
// yang sama, itu justru anomali yang harus dilaporkan — bukan dipilih salah
// satunya diam-diam.
type JournalDocumentResolver struct{}

func NewJournalDocumentResolver() *JournalDocumentResolver { return &JournalDocumentResolver{} }

var _ document.BusinessDocumentResolver = (*JournalDocumentResolver)(nil)

func (JournalDocumentResolver) DocumentForJournal(tx *gorm.DB, tenantID, journalID uint64) (uint64, error) {
	var ids []uint64
	err := tx.Raw(`
		SELECT d.id
		  FROM documents        d
		  JOIN receipts         r  ON r.id = d.source_id       AND r.tenant_id = d.tenant_id
		  JOIN termin_payments  tp ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
		 WHERE d.tenant_id = ? AND d.source_table = 'receipts' AND tp.journal_entry_id = ?`,
		tenantID, journalID).Scan(&ids).Error
	if err != nil {
		return 0, fmt.Errorf("cari kwitansi jurnal %d: %w", journalID, err)
	}
	switch len(ids) {
	case 0:
		return 0, nil
	case 1:
		return ids[0], nil
	default:
		return 0, fmt.Errorf("jurnal %d ditunjuk %d kwitansi — ambigu, tangani manual", journalID, len(ids))
	}
}
