//go:build integration

package document_test

// W-3.3 — backfill dokumen kas historis terhadap MySQL nyata.
//
// Skenario meniru data pra-W-3.2: jurnal kas yang sudah terposting tanpa
// dokumen, satu di antaranya sebenarnya SUDAH punya kwitansi yang tinggal
// ditautkan. Yang dibuktikan:
//
//	B-1  dry-run tidak menyentuh apa pun (tidak ada nomor yang terbakar)
//	B-2  jurnal berkwitansi DITAUTKAN, bukan diberi dokumen kedua
//	B-3  jenis dokumen retro mengikuti arah kas + asal jurnal
//	B-4  saldo awal dikecualikan — posisi kas, bukan pergerakan kas
//	B-5  pembalik dapat JR yang menunjuk dokumen aslinya
//	B-6  idempoten: jalan kedua kali tidak menemukan apa pun lagi
//	B-7  jurnal yang tidak bisa disimpulkan DITANDAI, bukan ditebak

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

const bfTenant uint64 = 9_900_250

// bfResolver adalah seam dokumen bisnis versi test: peta jurnal → dokumen yang
// sudah terbit. Implementasi produksinya (billing.JournalDocumentResolver)
// menelusuri receipts → termin_payments; yang diuji di sini adalah PERILAKU
// backfill terhadap jawaban resolver, bukan query kwitansinya.
type bfResolver map[uint64]uint64

func (m bfResolver) DocumentForJournal(_ *gorm.DB, _ uint64, journalID uint64) (uint64, error) {
	return m[journalID], nil
}

func bfSetup(t *testing.T) *gorm.DB {
	t.Helper()
	db := docConnect(t)
	clean := func() {
		for _, tbl := range []string{
			"documents", "document_sequences", "document_types",
			"journal_lines", "journal_entries", "accounts",
		} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", bfTenant).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tbl, err)
			}
		}
	}
	clean()
	t.Cleanup(clean)
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, bfTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	return db
}

// bfAccounts menanam COA minimal: satu bank, satu kas, satu lawan non-kas.
func bfAccounts(t *testing.T, db *gorm.DB) (bank, kas, lawan uint64) {
	t.Helper()
	accs := []*ledger.Account{
		{TenantID: bfTenant, Code: "1-1300", Name: "Bank BF", Type: domain.AccountAsset,
			NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
		{TenantID: bfTenant, Code: "1-1100", Name: "Kas BF", Type: domain.AccountAsset,
			NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryCash},
		{TenantID: bfTenant, Code: "5-3000", Name: "Beban BF", Type: domain.AccountExpense,
			NormalBalance: domain.NormalBalanceDebit, IsActive: true},
	}
	for _, a := range accs {
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("seed akun %s: %v", a.Code, err)
		}
	}
	return accs[0].ID, accs[1].ID, accs[2].ID
}

type bfLine struct {
	account uint64
	debit   string
	credit  string
}

// bfPostedJournal menyisipkan jurnal TERPOSTING tanpa dokumen — persis bentuk
// data pra-W-3.2 yang harus dibereskan backfill.
func bfPostedJournal(t *testing.T, db *gorm.DB, source string, reverses *uint64, lines ...bfLine) uint64 {
	t.Helper()
	rev := 0
	if reverses != nil {
		rev = 1
	}
	if err := db.Exec(`
		INSERT INTO journal_entries (tenant_id, date, description, reference, posted_at,
			is_reversing, reverses_id, source, created_at, updated_at)
		VALUES (?, NOW(3), ?, '', NOW(3), ?, ?, ?, NOW(3), NOW(3))`,
		bfTenant, "jurnal "+source, rev, reverses, source).Error; err != nil {
		t.Fatalf("buat jurnal: %v", err)
	}
	var id uint64
	if err := db.Raw(`SELECT LAST_INSERT_ID()`).Scan(&id).Error; err != nil {
		t.Fatalf("baca id jurnal: %v", err)
	}
	for _, l := range lines {
		if err := db.Exec(`
			INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit,
				description, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, '', NOW(3), NOW(3))`,
			bfTenant, id, l.account, l.debit, l.credit).Error; err != nil {
			t.Fatalf("buat baris jurnal: %v", err)
		}
	}
	return id
}

func bfDocOf(t *testing.T, db *gorm.DB, journalID uint64) *document.Document {
	t.Helper()
	doc, err := document.FindByJournal(db, bfTenant, journalID)
	if err != nil {
		t.Fatalf("FindByJournal %d: %v", journalID, err)
	}
	return doc
}

func TestW33_Backfill_MenutupSeluruhJurnalKasLama(t *testing.T) {
	db := bfSetup(t)
	bank, kas, lawan := bfAccounts(t, db)
	ctx := context.Background()

	// Data pra-W-3.2.
	jMasuk := bfPostedJournal(t, db, "system", nil,
		bfLine{bank, "5000000", "0"}, bfLine{lawan, "0", "5000000"})
	jKeluar := bfPostedJournal(t, db, "system", nil,
		bfLine{lawan, "750000", "0"}, bfLine{bank, "0", "750000"})
	jRefund := bfPostedJournal(t, db, "cancellation", nil,
		bfLine{lawan, "1000000", "0"}, bfLine{bank, "0", "1000000"})
	jSaldoAwal := bfPostedJournal(t, db, document.SourceOpeningBalance, nil,
		bfLine{bank, "9000000", "0"}, bfLine{lawan, "0", "9000000"})
	jTransfer := bfPostedJournal(t, db, "system", nil,
		bfLine{kas, "2000000", "0"}, bfLine{bank, "0", "2000000"})
	jPembalik := bfPostedJournal(t, db, "reversal", &jKeluar,
		bfLine{bank, "750000", "0"}, bfLine{lawan, "0", "750000"})
	jNol := bfPostedJournal(t, db, "system", nil, bfLine{bank, "0", "0"})

	// Jurnal yang kwitansinya SUDAH terbit — hanya tautannya yang hilang.
	jBerkwitansi := bfPostedJournal(t, db, "system", nil,
		bfLine{bank, "3000000", "0"}, bfLine{lawan, "0", "3000000"})
	var kwitansi *document.Document
	if err := db.Transaction(func(tx *gorm.DB) error {
		var e error
		kwitansi, e = document.Issue(tx, bfTenant, document.TypeHousePayment, time.Now(),
			document.Source{Table: "receipts", ID: 4242}, jlMoney(t, "3000000"), nil)
		return e
	}); err != nil {
		t.Fatalf("terbitkan kwitansi lama: %v", err)
	}
	resolver := bfResolver{jBerkwitansi: kwitansi.ID}

	// ── B-1 dry-run tidak menulis apa pun ────────────────────────────────────
	plan, err := document.BackfillCashDocuments(ctx, db, bfTenant, false, resolver)
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if plan.Scanned != 8 || plan.Exempt != 1 || plan.Linked != 1 || plan.Flagged != 1 {
		t.Fatalf("rencana salah: %+v", plan)
	}
	if plan.IssuedTotal() != 5 {
		t.Fatalf("rencana dokumen retro = %d, want 5 (%+v)", plan.IssuedTotal(), plan.Issued)
	}
	var docsSetelahDryRun int64
	db.Raw(`SELECT COUNT(*) FROM documents WHERE tenant_id = ?`, bfTenant).Scan(&docsSetelahDryRun)
	if docsSetelahDryRun != 1 {
		t.Fatalf("dry-run menerbitkan dokumen: ada %d, want 1 (hanya kwitansi lama)", docsSetelahDryRun)
	}
	if bfDocOf(t, db, jMasuk) != nil {
		t.Fatal("dry-run menulis tautan jurnal")
	}

	// ── APPLY ────────────────────────────────────────────────────────────────
	rep, err := document.BackfillCashDocuments(ctx, db, bfTenant, true, resolver)
	if err != nil {
		t.Fatalf("apply: %v", err)
	}
	if rep.Linked != 1 || rep.IssuedTotal() != 5 || rep.Flagged != 1 || rep.Exempt != 1 {
		t.Fatalf("hasil apply salah: %+v", rep)
	}

	// B-2 jurnal berkwitansi ditautkan ke dokumen yang SUDAH ada — bukan kedua.
	if got := bfDocOf(t, db, jBerkwitansi); got == nil || got.ID != kwitansi.ID {
		t.Fatalf("jurnal berkwitansi tidak tertaut ke dokumen aslinya: %+v", got)
	}
	var jmlKwitansi int64
	db.Raw(`SELECT COUNT(*) FROM documents WHERE tenant_id = ? AND source_table = 'receipts'`,
		bfTenant).Scan(&jmlKwitansi)
	if jmlKwitansi != 1 {
		t.Fatalf("kwitansi berganda: %d", jmlKwitansi)
	}

	// B-3 jenis dokumen retro mengikuti arah kas + asal jurnal.
	for _, c := range []struct {
		journal uint64
		want    string
		amount  string
	}{
		{jMasuk, document.TypeCashIn, "5000000"},
		{jKeluar, document.TypeCashOut, "750000"},
		{jRefund, document.TypeCustomerRefund, "1000000"},
		{jTransfer, document.TypeInternalTransfer, "2000000"},
		{jPembalik, document.TypeJournalReversal, "750000"},
	} {
		doc := bfDocOf(t, db, c.journal)
		if doc == nil {
			t.Fatalf("jurnal %d tidak berdokumen", c.journal)
		}
		if doc.DocumentTypeCode != c.want {
			t.Errorf("jurnal %d → %s, want %s", c.journal, doc.DocumentTypeCode, c.want)
		}
		if doc.Amount.String() != c.amount {
			t.Errorf("jurnal %d nominal = %s, want %s", c.journal, doc.Amount, c.amount)
		}
		// Penelusuran dua arah.
		jid, err := document.FindJournalID(db, bfTenant, doc.ID)
		if err != nil || jid != c.journal {
			t.Errorf("Document→Journal %s: %d (err=%v), want %d", doc.Number, jid, err, c.journal)
		}
	}

	// B-4 saldo awal tetap tanpa dokumen — dan itu memang benar.
	if doc := bfDocOf(t, db, jSaldoAwal); doc != nil {
		t.Errorf("saldo awal tidak boleh berdokumen: %s", doc.Number)
	}

	// B-5 dokumen pembalik menunjuk dokumen aslinya.
	revDoc, asliDoc := bfDocOf(t, db, jPembalik), bfDocOf(t, db, jKeluar)
	if revDoc.ReversesDocumentID == nil || *revDoc.ReversesDocumentID != asliDoc.ID {
		t.Errorf("JR %s tidak menunjuk dokumen asli %s", revDoc.Number, asliDoc.Number)
	}

	// B-7 jurnal nol ditandai, tidak ditebak.
	if doc := bfDocOf(t, db, jNol); doc != nil {
		t.Errorf("jurnal kas nol tidak boleh berdokumen: %s", doc.Number)
	}
	if len(rep.Flags) != 1 || rep.Flags[0].JournalID != jNol {
		t.Errorf("jurnal nol harus ditandai, flags = %+v", rep.Flags)
	}

	// ── B-6 idempoten ────────────────────────────────────────────────────────
	ulang, err := document.BackfillCashDocuments(ctx, db, bfTenant, true, resolver)
	if err != nil {
		t.Fatalf("jalan ulang: %v", err)
	}
	// Yang tersisa hanya saldo awal (dikecualikan) dan jurnal nol (ditandai) —
	// keduanya memang tidak boleh mendapat dokumen, berapa kali pun dijalankan.
	if ulang.Linked != 0 || ulang.IssuedTotal() != 0 {
		t.Fatalf("jalan kedua menghasilkan pekerjaan baru: %+v", ulang)
	}
	if ulang.Scanned != 2 || ulang.Exempt != 1 || ulang.Flagged != 1 {
		t.Fatalf("sisa pindaian tidak sesuai: %+v", ulang)
	}
	var totalDoc int64
	db.Raw(`SELECT COUNT(*) FROM documents WHERE tenant_id = ?`, bfTenant).Scan(&totalDoc)
	if totalDoc != 6 {
		t.Fatalf("total dokumen = %d, want 6 (1 kwitansi + 5 retro)", totalDoc)
	}
}
