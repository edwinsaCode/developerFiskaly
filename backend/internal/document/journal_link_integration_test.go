//go:build integration

package document_test

// W-3.1 — tautan Jurnal ↔ Dokumen terhadap MySQL nyata.
//
// Yang diuji adalah lima larangan INV-DOC-1 yang bisa dibuktikan di level ini:
//
//	L-1  satu cash movement menghasilkan dua dokumen        → ditolak
//	L-2  satu dokumen dipakai untuk dua cash movement       → ditolak (uk_je_document)
//	L-3  nomor terpakai akibat transaksi gagal/rollback     → seri kembali utuh
//	L-4  penelusuran dua arah Document ↔ Journal            → terbaca dua-duanya
//	L-5  dokumen pembalik menunjuk dokumen asli             → tercatat
//
// Sisanya ("jurnal kas terposting tanpa dokumen") baru bisa ditegakkan setelah
// seluruh jalur kas tersambung (W-3.2) dan penjaga fail-closed terpasang (W-3.5).

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
)

const jlTenant uint64 = 9_900_248

// jlJournal membuat satu jurnal terposting apa adanya (tanpa baris) — yang
// diuji di sini tautannya, bukan pembukuannya.
func jlJournal(t *testing.T, db *gorm.DB, desc string) uint64 {
	t.Helper()
	if err := db.Exec(`
		INSERT INTO journal_entries (tenant_id, date, description, reference, posted_at,
			is_reversing, source, created_at, updated_at)
		VALUES (?, NOW(3), ?, '', NOW(3), 0, 'manual', NOW(3), NOW(3))`,
		jlTenant, desc).Error; err != nil {
		t.Fatalf("buat jurnal: %v", err)
	}
	var id uint64
	if err := db.Raw(`SELECT LAST_INSERT_ID()`).Scan(&id).Error; err != nil {
		t.Fatalf("baca id jurnal: %v", err)
	}
	return id
}

func jlSetup(t *testing.T) *gorm.DB {
	t.Helper()
	db := docConnect(t)
	clean := func() {
		for _, tbl := range []string{"documents", "document_sequences", "document_types", "journal_entries"} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", jlTenant).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tbl, err)
			}
		}
	}
	clean()
	t.Cleanup(clean)
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, jlTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	return db
}

func jlMoney(t *testing.T, s string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(s)
	if err != nil {
		t.Fatalf("money %q: %v", s, err)
	}
	return m
}

// L-4 — dokumen kas terbit dari jurnalnya dan terbaca dari kedua arah.
func TestW31_IssueForJournal_TerbacaDuaArah(t *testing.T) {
	db := jlSetup(t)
	journalID := jlJournal(t, db, "Setoran modal pemilik")

	var doc *document.Document
	err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		doc, e = document.IssueForJournal(tx, jlTenant, document.TypeCashIn, time.Now(),
			journalID, jlMoney(t, "500000000"), nil)
		return e
	})
	if err != nil {
		t.Fatalf("IssueForJournal: %v", err)
	}
	if doc.DocumentTypeCode != document.TypeCashIn || doc.Number == "" {
		t.Fatalf("dokumen tidak sesuai: %+v", doc)
	}

	// Journal → Document
	got, err := document.FindByJournal(db, jlTenant, journalID)
	if err != nil || got == nil {
		t.Fatalf("FindByJournal: %v (doc=%v)", err, got)
	}
	if got.ID != doc.ID {
		t.Fatalf("dokumen jurnal salah: %d != %d", got.ID, doc.ID)
	}
	// Document → Journal
	jid, err := document.FindJournalID(db, jlTenant, doc.ID)
	if err != nil {
		t.Fatalf("FindJournalID: %v", err)
	}
	if jid != journalID {
		t.Fatalf("jurnal dokumen salah: %d != %d", jid, journalID)
	}
}

// L-1 — jurnal yang sudah berdokumen tidak boleh mendapat dokumen kedua.
func TestW31_JurnalTidakBolehBerdokumenDua(t *testing.T) {
	db := jlSetup(t)
	journalID := jlJournal(t, db, "Bayar vendor")

	if err := db.Transaction(func(tx *gorm.DB) error {
		_, e := document.IssueForJournal(tx, jlTenant, document.TypeCashOut, time.Now(),
			journalID, jlMoney(t, "1000000"), nil)
		return e
	}); err != nil {
		t.Fatalf("dokumen pertama: %v", err)
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		_, e := document.IssueForJournal(tx, jlTenant, document.TypeCashOut, time.Now(),
			journalID, jlMoney(t, "1000000"), nil)
		return e
	})
	if !errors.Is(err, document.ErrJournalNotLinkable) {
		t.Fatalf("expected ErrJournalNotLinkable, got %v", err)
	}
}

// L-2 — satu dokumen tidak boleh menjadi bukti dua jurnal.
func TestW31_DokumenTidakBolehDipakaiDuaJurnal(t *testing.T) {
	db := jlSetup(t)
	first := jlJournal(t, db, "Bayar PDAM")
	second := jlJournal(t, db, "Bayar listrik")

	var doc *document.Document
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		doc, e = document.IssueForJournal(tx, jlTenant, document.TypeThirdPartyPayout, time.Now(),
			first, jlMoney(t, "750000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan BTP: %v", err)
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		return document.LinkJournal(tx, jlTenant, doc.ID, second)
	})
	if !errors.Is(err, document.ErrJournalNotLinkable) {
		t.Fatalf("expected ErrJournalNotLinkable, got %v", err)
	}
}

// L-3 — transaksi gagal tidak boleh menghabiskan nomor.
//
// Inilah alasan Allocate wajib berada di transaksi pemanggil: kalau seri naik di
// luar transaksi, setiap kegagalan meninggalkan lubang nomor yang tidak bisa
// dijelaskan ke auditor.
func TestW31_TransaksiGagal_NomorTidakTerpakai(t *testing.T) {
	db := jlSetup(t)
	journalID := jlJournal(t, db, "Refund customer")
	boom := errors.New("gagal setelah nomor diambil")

	err := db.Transaction(func(tx *gorm.DB) error {
		if _, e := document.IssueForJournal(tx, jlTenant, document.TypeCustomerRefund, time.Now(),
			journalID, jlMoney(t, "2500000"), nil); e != nil {
			return e
		}
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("expected boom, got %v", err)
	}

	var docs int64
	if err := db.Model(&document.Document{}).
		Where("tenant_id = ? AND document_type_code = ?", jlTenant, document.TypeCustomerRefund).
		Count(&docs).Error; err != nil {
		t.Fatalf("hitung dokumen: %v", err)
	}
	if docs != 0 {
		t.Fatalf("dokumen harus ikut dibatalkan; ada %d", docs)
	}
	var last uint64
	if err := db.Raw(`SELECT COALESCE(MAX(last_val), 0) FROM document_sequences
		WHERE tenant_id = ? AND document_type_code = ?`,
		jlTenant, document.TypeCustomerRefund).Scan(&last).Error; err != nil {
		t.Fatalf("baca seri: %v", err)
	}
	if last != 0 {
		t.Fatalf("seri tidak boleh naik oleh transaksi gagal; last_val=%d", last)
	}

	// Setelah rollback, nomor pertama masih tersedia utuh.
	var doc *document.Document
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		doc, e = document.IssueForJournal(tx, jlTenant, document.TypeCustomerRefund, time.Now(),
			journalID, jlMoney(t, "2500000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan ulang: %v", err)
	}
	if doc.SequenceNo != 1 {
		t.Fatalf("nomor pertama harus 1, got %d (%s)", doc.SequenceNo, doc.Number)
	}
}

// L-5 — pembalikan mendapat dokumen JR sendiri yang menunjuk dokumen asli.
func TestW31_IssueReversal_MenunjukDokumenAsli(t *testing.T) {
	db := jlSetup(t)
	originalJournal := jlJournal(t, db, "Bayar subkontraktor")
	reversingJournal := jlJournal(t, db, "Pembalik bayar subkontraktor")

	var original, reversal *document.Document
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		original, e = document.IssueForJournal(tx, jlTenant, document.TypeCashOut, time.Now(),
			originalJournal, jlMoney(t, "80000000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan BKK: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		reversal, e = document.IssueReversal(tx, jlTenant, time.Now(),
			reversingJournal, original.ID, jlMoney(t, "80000000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan JR: %v", err)
	}

	if reversal.DocumentTypeCode != document.TypeJournalReversal {
		t.Fatalf("jenis dokumen pembalik salah: %s", reversal.DocumentTypeCode)
	}
	var stored document.Document
	if err := db.Where("id = ? AND tenant_id = ?", reversal.ID, jlTenant).First(&stored).Error; err != nil {
		t.Fatalf("baca dokumen pembalik: %v", err)
	}
	if stored.ReversesDocumentID == nil || *stored.ReversesDocumentID != original.ID {
		t.Fatalf("dokumen pembalik harus menunjuk %d, got %v", original.ID, stored.ReversesDocumentID)
	}
	// Dokumen asli tetap utuh — ledger append-only, bukti lama tidak dihapus.
	var still document.Document
	if err := db.Where("id = ? AND tenant_id = ?", original.ID, jlTenant).First(&still).Error; err != nil {
		t.Fatalf("dokumen asli hilang: %v", err)
	}
}
