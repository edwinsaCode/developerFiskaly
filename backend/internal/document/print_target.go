package document

// ResolvePrintTarget — jawaban atas "nomor dokumen ini, kalau diklik, harus
// mencetak APA?" (bukan "arahkan ke layar mana").
//
// Kwitansi/invoice punya cetakan langsung: SourceID di registry ini SUDAH
// jadi id entitasnya sendiri (receipts.id / invoices.id).
//
// Dokumen kas (BKK/BKM/BTP/RFC/KWD/JR) terbit lewat jurnal bersama — SourceID
// di sini adalah journal_entries.id, BUKAN id entitas yang punya cetakan.
// Package ini tidak boleh import `ap`/`cost` (mereka yang import `document`,
// bukan sebaliknya — lihat aturan impor CLAUDE.md), jadi pencarian pemilik
// sebenarnya dilakukan lewat SQL mentah ke tabelnya langsung, sama seperti
// appendAudit menulis ke master_data_changes tanpa import package `charge`.
import (
	"context"
	"fmt"
)

// PrintTargetKind — jenis cetakan yang sudah ADA di aplikasi untuk nomor ini.
type PrintTargetKind string

const (
	PrintReceipt   PrintTargetKind = "receipt"    // billing: fetchReceiptPrintHTML
	PrintInvoice   PrintTargetKind = "invoice"    // billing: fetchInvoicePrintHTML
	PrintAPPayment PrintTargetKind = "ap_payment" // ap: fetchAPPaymentPrintHTML
	PrintExpense   PrintTargetKind = "expense"    // cost: fetchExpensePrintHTML
	PrintUnknown   PrintTargetKind = "unknown"    // belum ada cetakan untuk sumber ini
)

type PrintTarget struct {
	Kind PrintTargetKind `json:"kind"`
	// ID — id yang dipakai endpoint cetak modul terkait (BUKAN id dokumen ini
	// sendiri, dan untuk kind=journal_entries yang tak terpetakan, kosong).
	ID uint64 `json:"id,omitempty"`
}

// ResolvePrintTargetByNumber mencari SATU dokumen persis dengan nomor ini lalu
// memetakannya ke entitas yang benar-benar bisa dicetak. Tidak menebak: nomor
// yang tak dikenal atau sumber yang belum punya cetakan pulang sebagai
// PrintUnknown, bukan dipetakan ke sesuatu yang mirip.
func (s *Service) ResolvePrintTargetByNumber(ctx context.Context, tenantID uint64, number string) (*PrintTarget, error) {
	docs, err := s.ListDocuments(ctx, tenantID, ListFilter{Number: number, Limit: 1})
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, ErrDocumentNotFound
	}
	return s.resolvePrintTarget(ctx, tenantID, docs[0])
}

func (s *Service) resolvePrintTarget(ctx context.Context, tenantID uint64, d *Document) (*PrintTarget, error) {
	switch d.SourceTable {
	case "receipts":
		return &PrintTarget{Kind: PrintReceipt, ID: d.SourceID}, nil
	case "invoices":
		return &PrintTarget{Kind: PrintInvoice, ID: d.SourceID}, nil
	case "journal_entries":
		return s.resolveJournalPrintTarget(ctx, tenantID, d.SourceID)
	default:
		return &PrintTarget{Kind: PrintUnknown}, nil
	}
}

// resolveJournalPrintTarget mencari pemilik jurnal kas ini di dua tempat yang
// diketahui punya cetakan sendiri: pembayaran vendor (ap_payments, BKK vendor)
// dan biaya yang dibayar tunai langsung (cost_entries, BKK non-vendor).
//
// Urutan sengaja: satu journal_entries hanya pernah dimiliki SATU baris
// sumber (INV-DOC-1 — satu pergerakan kas, satu dokumen), jadi begitu salah
// satu ditemukan, pencarian berhenti di situ.
//
// Jurnal kas yang bukan keduanya (mis. setoran modal manual, transfer antar
// kas) belum punya cetakan di modul mana pun — pulang sebagai PrintUnknown,
// bukan ditebak.
func (s *Service) resolveJournalPrintTarget(ctx context.Context, tenantID, journalID uint64) (*PrintTarget, error) {
	var apPaymentID uint64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT id FROM ap_payments WHERE tenant_id = ? AND journal_entry_id = ? LIMIT 1`,
		tenantID, journalID).Scan(&apPaymentID).Error; err != nil {
		return nil, fmt.Errorf("cari pembayaran vendor untuk jurnal %d: %w", journalID, err)
	}
	if apPaymentID != 0 {
		return &PrintTarget{Kind: PrintAPPayment, ID: apPaymentID}, nil
	}

	var costEntryID uint64
	if err := s.db.WithContext(ctx).Raw(`
		SELECT id FROM cost_entries WHERE tenant_id = ? AND journal_entry_id = ? LIMIT 1`,
		tenantID, journalID).Scan(&costEntryID).Error; err != nil {
		return nil, fmt.Errorf("cari biaya tunai untuk jurnal %d: %w", journalID, err)
	}
	if costEntryID != 0 {
		return &PrintTarget{Kind: PrintExpense, ID: costEntryID}, nil
	}

	return &PrintTarget{Kind: PrintUnknown}, nil
}
