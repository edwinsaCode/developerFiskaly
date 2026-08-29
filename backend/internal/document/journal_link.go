package document

// W-3.1 — tautan Jurnal ↔ Dokumen (INV-DOC-1).
//
// Acceptance criterion owner (FINAL 2026-08-07):
//
//	Setiap cash movement yang diposting harus memiliki TEPAT SATU Document
//	bernomor yang valid dan dapat ditelusuri dua arah.
//
// Semua penulisan tautan lewat berkas ini. Alasannya sama dengan alasan
// penomoran hanya boleh di engine.go: begitu ada dua tempat yang menulis
// `journal_entries.document_id`, salah satunya cepat atau lambat akan lupa
// memeriksa bahwa jurnalnya belum bertaut.
//
// Arah ketergantungan: `document` menyentuh tabel ledger lewat SQL mentah dan
// TIDAK pernah import package `ledger` — dengan begitu `ledger` bebas import
// `document` (W-3.2: `ledger/document_issuer.go` memanggil IssueForJournal saat
// posting jurnal kas) tanpa membuat siklus. Aturannya: satu arah import,
// `ledger` → `document`, tidak pernah sebaliknya.

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
)

// SourceJournal adalah nilai `source_table` untuk dokumen yang lahir langsung
// dari sebuah jurnal (BKM/BKK/BTP/RFC/JR) — bukan dari baris bisnis seperti
// receipts atau invoices.
const SourceJournal = "journal_entries"

// LinkJournal menautkan dokumen yang SUDAH terbit ke sebuah jurnal.
//
// Dipakai jalur yang dokumennya lahir dari baris bisnis (KWT dari `receipts`,
// KWD dari pencairan, …): dokumennya sudah ada, yang kurang hanya tautan ke
// jurnal kasnya.
//
// WAJIB dipanggil di transaksi yang sama dengan penerbitan dokumen dan posting
// jurnalnya. Guard `document_id IS NULL` di klausa WHERE adalah penegak larangan
// "satu cash movement menghasilkan dua dokumen": percobaan kedua tidak menimpa,
// melainkan gagal.
func LinkJournal(tx *gorm.DB, tenantID, documentID, journalID uint64) error {
	if journalID == 0 {
		return ErrJournalRequired
	}
	if documentID == 0 {
		return fmt.Errorf("%w: id dokumen kosong", ErrSourceRequired)
	}
	res := tx.Exec(`
		UPDATE journal_entries
		   SET document_id = ?, updated_at = NOW(3)
		 WHERE id = ? AND tenant_id = ? AND document_id IS NULL`,
		documentID, journalID, tenantID)
	if res.Error != nil {
		if isDuplicateKey(res.Error) {
			// uk_je_document: dokumen ini sudah menjadi bukti jurnal LAIN.
			return fmt.Errorf("%w: dokumen %d sudah dipakai jurnal lain", ErrJournalNotLinkable, documentID)
		}
		return fmt.Errorf("tautkan jurnal %d ke dokumen %d: %w", journalID, documentID, res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("%w: jurnal %d (tidak ada, milik tenant lain, atau sudah berdokumen)",
			ErrJournalNotLinkable, journalID)
	}
	return nil
}

// IssueForJournal menerbitkan dokumen yang asalnya adalah jurnal itu sendiri,
// lalu menautkannya — satu panggilan untuk jalur kas yang tidak punya baris
// bisnis tersendiri (kas masuk lain-lain, pengeluaran operasional, pembalikan).
//
// `documents.source` menunjuk balik ke jurnalnya, sehingga arah Document →
// Journal tetap terbaca meski `journal_entries` dibaca terpisah.
func IssueForJournal(tx *gorm.DB, tenantID uint64, typeCode string, issuedAt time.Time,
	journalID uint64, amount domain.Money, createdBy *uint64) (*Document, error) {
	if journalID == 0 {
		return nil, ErrJournalRequired
	}
	doc, err := Issue(tx, tenantID, typeCode, issuedAt,
		Source{Table: SourceJournal, ID: journalID}, amount, createdBy)
	if err != nil {
		if errors.Is(err, ErrSourceHasDocument) {
			// Jurnal ini sudah punya dokumen — larangan yang sama dengan yang
			// dijaga guard `document_id IS NULL` di LinkJournal, hanya tertangkap
			// lebih dulu oleh uk_doc_source. Pemanggil cukup memeriksa satu
			// sentinel; dua sentinel untuk satu larangan membuat separuh
			// pemanggil lupa memeriksa yang satunya.
			return nil, fmt.Errorf("%w: jurnal %d sudah berdokumen (%w)", ErrJournalNotLinkable, journalID, err)
		}
		return nil, err
	}
	if err := LinkJournal(tx, tenantID, doc.ID, journalID); err != nil {
		return nil, err
	}
	return doc, nil
}

// IssueReversal menerbitkan dokumen pembalik (jenis JR) untuk jurnal pembalik,
// menunjuk dokumen asli yang dibalik.
//
// D-W3-5: pembalikan TIDAK memakai ulang nomor dokumen asli. Ledger bersifat
// append-only (Invariant #5) — dokumen asli tetap sah sebagai bukti bahwa kas
// pernah bergerak; yang baru adalah bukti bahwa gerakan itu dibatalkan.
// `originalDocumentID` boleh 0 bila jurnal aslinya memang tidak berdokumen
// (data pra-W-3, jurnal non-kas): pembaliknya tetap dapat dokumen sendiri.
func IssueReversal(tx *gorm.DB, tenantID uint64, issuedAt time.Time,
	reversingJournalID, originalDocumentID uint64, amount domain.Money, createdBy *uint64) (*Document, error) {
	doc, err := IssueForJournal(tx, tenantID, TypeJournalReversal, issuedAt,
		reversingJournalID, amount, createdBy)
	if err != nil {
		return nil, err
	}
	if originalDocumentID != 0 {
		if err := tx.Model(&Document{}).
			Where("id = ? AND tenant_id = ?", doc.ID, tenantID).
			Update("reverses_document_id", originalDocumentID).Error; err != nil {
			return nil, fmt.Errorf("tautkan dokumen pembalik %s: %w", doc.Number, err)
		}
		doc.ReversesDocumentID = &originalDocumentID
	}
	return doc, nil
}

// FindBySource mengembalikan dokumen yang terbit dari sebuah baris bisnis
// (receipts, invoices, charge_settlements, …). nil, nil bila belum ada.
//
// Dipakai jalur yang dokumennya lahir lebih dulu dari jurnalnya: pemanggil
// membuat kwitansi, mencari dokumennya di sini, lalu LinkJournal ke jurnal
// kasnya — semua dalam satu transaksi.
func FindBySource(tx *gorm.DB, tenantID uint64, table string, id uint64) (*Document, error) {
	if strings.TrimSpace(table) == "" || id == 0 {
		return nil, fmt.Errorf("%w: %q/%d", ErrSourceRequired, table, id)
	}
	var doc Document
	err := tx.Where("tenant_id = ? AND source_table = ? AND source_id = ?", tenantID, strings.TrimSpace(table), id).
		First(&doc).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("baca dokumen sumber %s#%d: %w", table, id, err)
	}
	return &doc, nil
}

// LinkJournalBySource menautkan jurnal ke dokumen milik sebuah baris bisnis.
// Gagal bila baris itu belum punya dokumen — jalur kas tidak boleh diam-diam
// melewati tautan hanya karena dokumennya tidak ketemu.
func LinkJournalBySource(tx *gorm.DB, tenantID uint64, table string, sourceID, journalID uint64) (*Document, error) {
	doc, err := FindBySource(tx, tenantID, table, sourceID)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return nil, fmt.Errorf("%w: %s#%d belum punya dokumen", ErrJournalNotLinkable, table, sourceID)
	}
	if err := LinkJournal(tx, tenantID, doc.ID, journalID); err != nil {
		return nil, err
	}
	return doc, nil
}

// FindByJournal mengembalikan dokumen sebuah jurnal — arah Journal → Document.
// nil, nil bila jurnalnya belum/tidak berdokumen (jurnal non-kas, saldo awal,
// atau data yang belum di-backfill).
func FindByJournal(tx *gorm.DB, tenantID, journalID uint64) (*Document, error) {
	var docID *uint64
	if err := tx.Raw(`SELECT document_id FROM journal_entries WHERE id = ? AND tenant_id = ?`,
		journalID, tenantID).Scan(&docID).Error; err != nil {
		return nil, fmt.Errorf("baca dokumen jurnal %d: %w", journalID, err)
	}
	if docID == nil || *docID == 0 {
		return nil, nil
	}
	var doc Document
	if err := tx.Where("id = ? AND tenant_id = ?", *docID, tenantID).First(&doc).Error; err != nil {
		return nil, fmt.Errorf("baca dokumen %d: %w", *docID, err)
	}
	return &doc, nil
}

// FindJournalID mengembalikan jurnal sebuah dokumen — arah Document → Journal.
// 0, nil bila dokumen belum tertaut ke jurnal (mis. invoice: tagihan bukan
// pergerakan kas).
func FindJournalID(tx *gorm.DB, tenantID, documentID uint64) (uint64, error) {
	var id uint64
	if err := tx.Raw(`SELECT COALESCE(MIN(id), 0) FROM journal_entries WHERE tenant_id = ? AND document_id = ?`,
		tenantID, documentID).Scan(&id).Error; err != nil {
		return 0, fmt.Errorf("baca jurnal dokumen %d: %w", documentID, err)
	}
	return id, nil
}
