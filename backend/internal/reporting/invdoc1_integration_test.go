//go:build integration

package reporting

// ═════════════════════════════════════════════════════════════════════════════
// EQ_INV_DOC_1 — INVARIANT DOKUMEN KAS (W-3.5)
//
//	Setiap cash movement yang diposting harus memiliki TEPAT SATU Document
//	bernomor yang valid dan dapat ditelusuri DUA ARAH:
//	Document → Journal dan Journal → Document.
//
// Suite ini menjalankan jalur kas PRODUKSI (booking, pelunasan termin,
// pencairan KPR, jurnal manual kas keluar, pembalik, titipan warisan, saldo
// awal) lalu memeriksa buku hasilnya — bukan memeriksa niat kodenya.
//
// Dua sisi diuji, karena satu sisi saja tidak membuktikan apa pun:
//   - sisi POSITIF  : buku yang dihasilkan jalur produksi utuh menurut audit;
//   - sisi NEGATIF  : jalur yang mencoba menggerakkan kas tanpa dokumen DITOLAK
//     dan tidak meninggalkan jejak apa pun (tidak posted, nomor tidak terpakai).
//
// Sisi negatif itulah yang membedakan invariant dari kebetulan: audit bersih
// pada buku yang memang tidak pernah diuji tekanannya hanya membuktikan bahwa
// jalur bahagia berjalan.
//
// Jalankan: docker compose exec -T backend sh -c \
//	'cd /app && TEST_DB_DSN="$DB_DSN" go test -tags integration ./internal/reporting/'
// ═════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"errors"
	"testing"
	"time"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
	"esaproperti/internal/scheme"
)

func TestIntegration_EQ_INV_DOC_1(t *testing.T) {
	env := eqSetup(t)
	ctx := context.Background()
	day := time.Now().Truncate(24 * time.Hour).UTC()

	// ── Skenario: sebar jalur kas seluas mungkin ──────────────────────────────

	// Non-kas (Dr 1-3100 / Cr 2-1000) — tidak boleh menuntut dokumen apa pun.
	env.postCost(t, 300_000_000, day)

	// Saldo awal: mendebit kas, tetapi menetapkan POSISI, bukan pergerakan.
	// Dikecualikan INV-DOC-1 — dan pengecualian itu harus tetap berlaku setelah
	// penegakan dinyalakan, kalau tidak tenant baru tidak bisa dibuka sama sekali.
	openingID := env.postOpeningBalance(t, 25_000_000, day)

	// Booking (KWB) → kontrak KPR → DP (KWT) → akad → pencairan (KWD).
	bk, err := env.svc.CreateBooking(ctx, eqTenant, sale.CreateBookingRequest{
		UnitID: env.unitA, CustomerID: env.customerID, SalesPersonID: &env.salesID,
		BookingFee: domain.FromInt(5_000_000), Refundable: true,
		BankAccountCode: "1-1300", BookingDate: day, ExpiryDate: day.AddDate(0, 1, 0),
	})
	if err != nil {
		t.Fatalf("booking: %v", err)
	}
	c, err := env.svc.CreateContract(ctx, eqTenant, sale.CreateContractRequest{
		UnitID: env.unitA, BuyerName: "Budi", BuyerID: "3201", ContractDate: day,
		TotalPrice:      domain.FromInt(500_000_000),
		PaymentSchemeID: &env.schemeKPRID, FinancingSourceID: &env.finSourceID,
		CustomerID: &env.customerID, SalesPersonID: &env.salesID, BookingID: &bk.ID,
	})
	if err != nil {
		t.Fatalf("kontrak: %v", err)
	}
	if _, err := env.svc.CreatePaymentSchedule(ctx, eqTenant, c.ID, []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: day.AddDate(0, 0, 20), Amount: domain.FromInt(100_000_000), Type: sale.ScheduleTypeDP},
		{InstallmentNumber: 2, DueDate: day.AddDate(0, 0, 80), Amount: domain.FromInt(400_000_000), Type: sale.ScheduleTypeFinal},
	}); err != nil {
		t.Fatalf("jadwal: %v", err)
	}
	env.pay(t, c.ID, 45_000_000, day, nil)
	env.event(t, c.ID, scheme.EventSubmittedToBank)
	env.event(t, c.ID, scheme.EventBankApproved)
	env.event(t, c.ID, scheme.EventAkad)
	env.pay(t, c.ID, 430_000_000, day, &env.finSourceID)

	// Titipan warisan pra-W-1 (BKM lewat backfill) — bentuk data produksi hari ini.
	seedLegacyNotaryDeposit(t, env, day, domain.FromInt(3_000_000))

	// Jurnal manual kas keluar (BKK) lalu dibalik (JR). Pembalik menggerakkan kas
	// juga, jadi ia butuh nomornya sendiri — bukan menumpang nomor yang dibalik.
	manualID := env.draftCashOut(t, 2_000_000, day)
	if _, err := env.posting.PostDraft(ctx, eqTenant, manualID,
		ledger.DocumentSpec{TypeCode: ledger.DocCashOut}); err != nil {
		t.Fatalf("posting kas keluar manual: %v", err)
	}
	if _, err := env.posting.Reverse(ctx, eqTenant, manualID, day); err != nil {
		t.Fatalf("membalik kas keluar manual: %v", err)
	}

	// ── Sisi positif: buku utuh menurut audit W-3.4 ───────────────────────────

	rep, err := document.AuditCashDocuments(ctx, env.db, eqTenant, 50)
	if err != nil {
		t.Fatalf("audit: %v", err)
	}
	if !rep.Clean {
		for _, c := range rep.Checks {
			for _, f := range c.Findings {
				t.Errorf("pelanggaran %s: jurnal=%d dokumen=%s — %s",
					c.Code, f.JournalID, f.Number, f.Detail)
			}
		}
		t.Fatalf("INV-DOC-1 dilanggar: %d temuan", rep.Violations)
	}
	if rep.CashPosted == 0 {
		t.Fatal("skenario tidak menghasilkan satu pun jurnal kas terposting — audit bersih tidak bermakna")
	}
	if rep.Documented+rep.Exempt != rep.CashPosted {
		t.Errorf("cakupan bocor: berdokumen %d + dikecualikan %d ≠ kas terposting %d",
			rep.Documented, rep.Exempt, rep.CashPosted)
	}
	if rep.Exempt == 0 {
		t.Error("saldo awal tidak terhitung sebagai pengecualian — skenario tidak menguji jalurnya")
	}

	// ── Penelusuran dua arah, baris per baris ─────────────────────────────────

	type link struct {
		JournalID  uint64 `gorm:"column:journal_id"`
		DocumentID uint64 `gorm:"column:document_id"`
		Number     string `gorm:"column:number"`
		TypeCode   string `gorm:"column:document_type_code"`
		SourceID   uint64 `gorm:"column:source_id"`
		SourceTbl  string `gorm:"column:source_table"`
	}
	var links []link
	if err := env.db.Raw(`
		SELECT j.id AS journal_id, d.id AS document_id, d.number,
		       d.document_type_code, COALESCE(d.source_id,0) AS source_id,
		       COALESCE(d.source_table,'') AS source_table
		  FROM journal_entries j
		  JOIN documents d ON d.id = j.document_id AND d.tenant_id = j.tenant_id
		 WHERE j.tenant_id = ? AND j.posted_at IS NOT NULL
		 ORDER BY j.id ASC`, eqTenant).Scan(&links).Error; err != nil {
		t.Fatalf("baca tautan: %v", err)
	}
	if len(links) != rep.Documented {
		t.Fatalf("tautan terbaca %d ≠ jurnal berdokumen menurut audit %d", len(links), rep.Documented)
	}
	seen := map[uint64]uint64{} // documentID → journalID
	for _, l := range links {
		if l.Number == "" {
			t.Errorf("jurnal %d menunjuk dokumen %d yang tidak bernomor", l.JournalID, l.DocumentID)
		}
		if prev, dup := seen[l.DocumentID]; dup {
			t.Errorf("dokumen %s dipakai dua jurnal: %d dan %d", l.Number, prev, l.JournalID)
		}
		seen[l.DocumentID] = l.JournalID
	}

	// Arah sebaliknya (Document → Journal) untuk seluruh dokumen kas: tidak boleh
	// ada nomor kas yang tidak dipegang jurnal terposting mana pun.
	var orphan int64
	if err := env.db.Raw(`
		SELECT COUNT(*)
		  FROM documents d
		  LEFT JOIN journal_entries j
		    ON j.tenant_id = d.tenant_id AND j.document_id = d.id AND j.posted_at IS NOT NULL
		 WHERE d.tenant_id = ? AND d.document_type_code NOT IN ('MTI','INV') AND j.id IS NULL`,
		eqTenant).Scan(&orphan).Error; err != nil {
		t.Fatalf("hitung dokumen yatim: %v", err)
	}
	if orphan != 0 {
		t.Errorf("%d dokumen kas bernomor tidak dipegang jurnal terposting mana pun", orphan)
	}

	// Saldo awal memang tidak ikut di sini — itu pengecualiannya, bukan celahnya.
	var openingLinked int64
	if err := env.db.Raw(`SELECT COUNT(*) FROM journal_entries
		 WHERE tenant_id = ? AND id = ? AND document_id IS NOT NULL`,
		eqTenant, openingID).Scan(&openingLinked).Error; err != nil {
		t.Fatalf("cek dokumen saldo awal: %v", err)
	}
	if openingLinked != 0 {
		t.Error("saldo awal justru menerbitkan dokumen — pengecualian berubah diam-diam")
	}

	// ── Sisi negatif: kas tanpa dokumen HARUS ditolak, tanpa jejak ────────────

	before := env.countRows(t, "documents")
	beforeSeq := env.sequenceState(t)

	blocked := env.draftCashOut(t, 7_000_000, day)
	if _, err := env.posting.PostDraft(ctx, eqTenant, blocked, ledger.DocumentSpec{}); err == nil {
		t.Fatal("jurnal kas terposting tanpa dokumen — INV-DOC-1 tidak ditegakkan")
	} else if !errors.Is(err, ledger.ErrDocumentRequired) {
		t.Fatalf("ditolak dengan alasan lain: %v", err)
	}
	// Jalur telanjang Post() harus berhenti di tempat yang sama.
	if _, err := env.posting.Post(ctx, eqTenant, blocked); !errors.Is(err, ledger.ErrDocumentRequired) {
		t.Fatalf("Post() telanjang tidak menegakkan INV-DOC-1: %v", err)
	}

	var stillPosted int64
	if err := env.db.Raw(`SELECT COUNT(*) FROM journal_entries
		 WHERE tenant_id = ? AND id = ? AND posted_at IS NOT NULL`,
		eqTenant, blocked).Scan(&stillPosted).Error; err != nil {
		t.Fatalf("cek status jurnal yang ditolak: %v", err)
	}
	if stillPosted != 0 {
		t.Error("penolakan meninggalkan jurnal dalam keadaan POSTED — justru melahirkan yang dilarang")
	}
	if after := env.countRows(t, "documents"); after != before {
		t.Errorf("penolakan tetap menerbitkan dokumen: %d → %d", before, after)
	}
	if after := env.sequenceState(t); after != beforeSeq {
		t.Errorf("penolakan membakar nomor: %q → %q", beforeSeq, after)
	}

	// Setelah semua tekanan itu, buku harus tetap utuh.
	rep2, err := document.AuditCashDocuments(ctx, env.db, eqTenant, 50)
	if err != nil {
		t.Fatalf("audit ulang: %v", err)
	}
	if !rep2.Clean {
		t.Errorf("buku tidak lagi utuh setelah percobaan yang ditolak: %d pelanggaran", rep2.Violations)
	}
}

// ── Helper khusus suite ini ──────────────────────────────────────────────────

// postOpeningBalance menanam saldo awal kas lewat jalur produksi (source =
// opening_balance) dan memastikan ia BOLEH diposting tanpa dokumen.
func (e *eqEnv) postOpeningBalance(t *testing.T, amount int64, day time.Time) uint64 {
	t.Helper()
	ctx := context.Background()
	ids := e.accountIDs(t, "1-1300", "3-1000")
	entry, err := e.posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: eqTenant, Date: day, Description: "Saldo awal kas (EQ_INV_DOC_1)",
		Source: ledger.SourceOpeningBalance,
		Lines: []ledger.LineInput{
			{AccountID: ids["1-1300"], Debit: domain.FromInt(amount)},
			{AccountID: ids["3-1000"], Credit: domain.FromInt(amount)},
		},
	})
	if err != nil {
		t.Fatalf("draft saldo awal: %v", err)
	}
	if _, err := e.posting.Post(ctx, eqTenant, entry.ID); err != nil {
		t.Fatalf("saldo awal ditolak penegakan INV-DOC-1: %v", err)
	}
	return entry.ID
}

// draftCashOut membuat DRAFT kas keluar (Dr beban / Cr bank) tanpa memposting.
func (e *eqEnv) draftCashOut(t *testing.T, amount int64, day time.Time) uint64 {
	t.Helper()
	ids := e.accountIDs(t, "1-1300", "5-3000")
	entry, err := e.posting.Create(context.Background(), ledger.CreateJournalRequest{
		TenantID: eqTenant, Date: day, Description: "Beban operasional (EQ_INV_DOC_1)",
		Source: "manual",
		Lines: []ledger.LineInput{
			{AccountID: ids["5-3000"], Debit: domain.FromInt(amount)},
			{AccountID: ids["1-1300"], Credit: domain.FromInt(amount)},
		},
	})
	if err != nil {
		t.Fatalf("draft kas keluar: %v", err)
	}
	return entry.ID
}

func (e *eqEnv) countRows(t *testing.T, table string) int64 {
	t.Helper()
	var n int64
	if err := e.db.Raw("SELECT COUNT(*) FROM "+table+" WHERE tenant_id = ?", eqTenant).
		Scan(&n).Error; err != nil {
		t.Fatalf("hitung %s: %v", table, err)
	}
	return n
}

// sequenceState memotret seluruh counter penomoran sebagai satu string —
// dipakai untuk membuktikan transaksi yang gagal tidak membakar nomor.
func (e *eqEnv) sequenceState(t *testing.T) string {
	t.Helper()
	var rows []string
	if err := e.db.Raw(`
		SELECT CONCAT(document_type_code,'/',fiscal_year,'=',last_val)
		  FROM document_sequences WHERE tenant_id = ?
		 ORDER BY document_type_code, fiscal_year`, eqTenant).Scan(&rows).Error; err != nil {
		t.Fatalf("baca sequence: %v", err)
	}
	out := ""
	for _, r := range rows {
		out += r + " "
	}
	return out
}
