//go:build integration

package legacyar_test

// W-7 — PIUTANG PROYEK LAMA (LEGACY AR).
//
// Piutang dari proyek yang selesai SEBELUM buku ini dibuka. Customer-nya tidak
// punya unit, kontrak, maupun jadwal cicilan di sistem — dan tidak dipaksa
// punya. Yang diuji suite ini adalah empat janji yang kalau dilanggar akan
// merusak laporan keuangan tanpa suara:
//
//	INV-LAR-1  Impor tidak pernah membuat jurnal (piutangnya sudah ada di GL
//	           lewat saldo awal; menjurnalnya lagi menggandakan aset).
//	INV-LAR-2  Rekonsiliasi berbasis NOMINAL terhadap porsi saldo awal akun
//	           kontrol — bukan berbasis jumlah baris.
//	INV-LAR-3  paid_amount hanyalah cache; legacy_receivable_payments SSOT.
//	INV-DOC-1  Setiap pergerakan kas punya tepat satu dokumen bernomor (BKM).
//
// Semuanya dijalankan lewat SERVICE produksi dengan wiring seperti
// cmd/api/main.go, karena yang paling mungkin rusak justru kabelnya.

import (
	"bytes"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/legacyar"
	"esaproperti/internal/receivable"
	"esaproperti/internal/reporting"
	"esaproperti/internal/sale"
)

const larTenant uint64 = 9_900_777

// larAsOf adalah tanggal posisi piutang lama — akhir tahun buku sebelumnya.
var larAsOf = time.Date(2025, 12, 31, 0, 0, 0, 0, time.UTC)

type larEnv struct {
	db  *gorm.DB
	svc *legacyar.Service
	rep *reporting.Service
}

func larConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func larCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"legacy_receivable_audits", "legacy_receivable_payments",
		"legacy_receivables", "legacy_ar_batch_rows", "legacy_ar_batches",
		"documents", "document_sequences", "document_types",
		"journal_lines", "journal_entries", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", larTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func larSetup(t *testing.T) *larEnv {
	t.Helper()
	db := larConnect(t)
	larCleanup(t, db)
	ctx := context.Background()
	if err := ledger.SeedCOA(ctx, db, larTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	db.Exec(`INSERT IGNORE INTO tenants (id, name) VALUES (?, 'Legacy AR Test')`, larTenant)
	// W-2: tanpa master jenis dokumen tidak ada BKM yang bisa terbit, dan
	// INV-DOC-1 akan menolak seluruh penerimaan kas — fail-closed, seperti di
	// produksi. Jadi seed-nya bagian dari setup, bukan fixture.
	if err := document.SeedDefaultDocumentTypes(ctx, db, larTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}

	svc := legacyar.NewService(legacyar.NewRepository(db), db).
		WithBalanceReader(ledger.NewLedgerBalanceService(ledger.NewQueryService(db)))

	// W-8: pembaca piutang harga rumah adalah sale.Service, dan ia WAJIB
	// terpasang seperti di produksi — tenant ini memang tidak punya penjualan
	// baru, jadi sisi house-nya nol karena tidak ada BAST, bukan karena
	// pembacanya absen.
	saleRepo := sale.NewGORMRepository(db, nil, nil)
	saleSvc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo), sale.WithHouseARStore(saleRepo))

	repRepo := reporting.NewGORMRepository(db)
	rep := reporting.NewService(ledger.NewQueryService(db), repRepo, repRepo, repRepo).
		WithHouseReader(saleSvc).
		WithLegacyReader(svc)

	return &larEnv{db: db, svc: svc, rep: rep}
}

// ── Fixture ──────────────────────────────────────────────────────────────────

// buildFile menghasilkan berkas Excel dari template RESMI yang diisi baris data.
// Sengaja lewat BuildTemplate, bukan workbook buatan tangan: yang harus terbukti
// adalah bahwa berkas yang benar-benar diunduh admin bisa dibaca kembali.
func buildFile(t *testing.T, rows [][]string) []byte {
	t.Helper()
	tpl, err := legacyar.BuildTemplate()
	if err != nil {
		t.Fatalf("BuildTemplate: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(tpl))
	if err != nil {
		t.Fatalf("buka template: %v", err)
	}
	defer f.Close()
	existing, err := f.GetRows(legacyar.TemplateSheet)
	if err != nil {
		t.Fatalf("baca sheet: %v", err)
	}
	start := len(existing) + 1
	for i, row := range rows {
		for c, val := range row {
			cell, _ := excelize.CoordinatesToCellName(c+1, start+i)
			if err := f.SetCellStr(legacyar.TemplateSheet, cell, val); err != nil {
				t.Fatalf("tulis sel: %v", err)
			}
		}
	}
	var out bytes.Buffer
	if err := f.Write(&out); err != nil {
		t.Fatalf("tulis workbook: %v", err)
	}
	return out.Bytes()
}

// threeCustomers adalah contoh dari brief klien: 300 + 250 + 450 = 1 miliar.
func threeCustomers() [][]string {
	return [][]string{
		{"Budi Santoso", "Perumahan Griya Asri (2019)", "REF-001", "300.000.000", "2025-06-30", "0811000001", "", ""},
		{"Andi Wijaya", "Perumahan Griya Asri (2019)", "REF-002", "250.000.000", "2025-09-30", "", "", ""},
		{"Citra Dewi", "Ruko Pasar Baru (2020)", "REF-003", "450.000.000", "2026-03-31", "", "", ""},
	}
}

// seedOpening memposting jurnal SALDO AWAL: Dr 1-2000 / Cr 3-1000 (modal).
// Inilah yang di produksi dibuat admin lewat Saldo Awal biasa — piutang lama
// SUDAH ada di buku besar sebelum impor, dan impor hanya merincinya.
func (e *larEnv) seedOpening(t *testing.T, amount int64) {
	t.Helper()
	ctx := context.Background()
	ar := e.accountID(t, "1-2000")
	eq := e.accountID(t, "3-1000")
	repo := ledger.NewGORMRepository(e.db)
	posting := ledger.NewPostingService(repo, repo).WithPeriodChecker(repo)
	_, err := posting.CreateAndPost(ctx, ledger.CreateJournalRequest{
		TenantID:    larTenant,
		Date:        larAsOf,
		Description: "Saldo Awal Piutang Customer",
		Source:      ledger.SourceOpeningBalance,
		Lines: []ledger.LineInput{
			{AccountID: ar, Debit: domain.FromInt(amount)},
			{AccountID: eq, Credit: domain.FromInt(amount)},
		},
	})
	if err != nil {
		t.Fatalf("post saldo awal: %v", err)
	}
}

func (e *larEnv) accountID(t *testing.T, code string) uint64 {
	t.Helper()
	var id uint64
	if err := e.db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = ?`,
		larTenant, code).Scan(&id).Error; err != nil || id == 0 {
		t.Fatalf("akun %s tidak ditemukan: %v", code, err)
	}
	return id
}

func (e *larEnv) accountBalance(t *testing.T, code string) domain.Money {
	t.Helper()
	bal, err := ledger.NewLedgerBalanceService(ledger.NewQueryService(e.db)).
		AccountBalance(context.Background(), larTenant, code, nil)
	if err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return bal
}

// importFile menjalankan alur produksi penuh: Upload → Preview → Commit.
func (e *larEnv) importFile(t *testing.T, rows [][]string, counterAccount string) *legacyar.CommitResult {
	t.Helper()
	ctx := context.Background()
	batch, err := e.svc.Upload(ctx, larTenant, legacyar.UploadRequest{
		FileName: "piutang-lama.xlsx",
		Content:  buildFile(t, rows),
		AsOfDate: larAsOf,
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	res, err := e.svc.Commit(ctx, larTenant, batch.ID, legacyar.CommitRequest{
		OpeningCounterAccountCode: counterAccount,
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return res
}

func (e *larEnv) openReceivables(t *testing.T) []legacyar.ReceivableView {
	t.Helper()
	out, err := e.svc.ListReceivables(context.Background(), larTenant, legacyar.ListFilter{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	return out.Rows
}

func money(n int64) domain.Money { return domain.FromInt(n) }

func mustEqual(t *testing.T, label string, got, want domain.Money) {
	t.Helper()
	if !got.Equal(want) {
		t.Errorf("%s = %s, want %s", label, got.String(), want.String())
	}
}

// mustEqualStr membandingkan nominal yang sudah diserialkan laporan sebagai
// string. Dibandingkan sebagai Money, bukan sebagai teks, supaya "1000000000"
// dan "1000000000.0000" tidak dianggap berbeda.
func mustEqualStr(t *testing.T, label, got string, want domain.Money) {
	t.Helper()
	m, err := domain.NewMoney(got)
	if err != nil {
		t.Errorf("%s = %q, bukan nominal yang sah: %v", label, got, err)
		return
	}
	if !m.Equal(want) {
		t.Errorf("%s = %s, want %s", label, m.String(), want.String())
	}
}

// ═══ Skenario A — impor 1 miliar, GL saldo awal 1 miliar → COCOK ═════════════
//
// Sekaligus skenario J: impor TIDAK boleh menambah satu jurnal pun.
func TestA_ImportMatchesLedger(t *testing.T) {
	e := larSetup(t)
	e.seedOpening(t, 1_000_000_000)

	var journalsBefore int64
	e.db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, larTenant).Scan(&journalsBefore)

	res := e.importFile(t, threeCustomers(), "")

	if res.ImportedCount != 3 {
		t.Fatalf("ImportedCount = %d, want 3", res.ImportedCount)
	}
	mustEqual(t, "TotalImported", res.TotalImported, money(1_000_000_000))

	// INV-LAR-2 — nominal, bukan jumlah baris.
	rec := res.Reconciliation
	mustEqual(t, "OpeningPortion", rec.OpeningPortion, money(1_000_000_000))
	mustEqual(t, "SubledgerTotal", rec.SubledgerTotal, money(1_000_000_000))
	mustEqual(t, "Difference", rec.Difference, domain.Zero)
	if !rec.Matched {
		t.Errorf("Matched = false, want true (selisih %s)", rec.Difference.String())
	}

	// INV-LAR-1 / skenario J — nol jurnal baru.
	var journalsAfter int64
	e.db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, larTenant).Scan(&journalsAfter)
	if journalsAfter != journalsBefore {
		t.Errorf("impor membuat %d jurnal baru, want 0", journalsAfter-journalsBefore)
	}
	if res.OpeningJournal != nil {
		t.Errorf("OpeningJournal = %v, want nil (tidak diminta)", *res.OpeningJournal)
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(1_000_000_000))
}

// ═══ Skenario B — impor 100jt, GL 80jt → selisih 20jt, tanpa jurnal karangan ══
func TestB_DifferenceIsReportedNotFabricated(t *testing.T) {
	e := larSetup(t)
	e.seedOpening(t, 80_000_000)

	var before int64
	e.db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, larTenant).Scan(&before)

	res := e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-B1", "100.000.000", "", "", "", ""},
	}, "")

	rec := res.Reconciliation
	// Buku besar KURANG 20jt dari rincian → selisih negatif.
	mustEqual(t, "Difference", rec.Difference, money(-20_000_000))
	if rec.Matched {
		t.Error("Matched = true padahal selisih 20jt")
	}

	var after int64
	e.db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, larTenant).Scan(&after)
	if after != before {
		t.Fatalf("selisih memicu %d jurnal otomatis — sistem tidak boleh mengarang jurnal", after-before)
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(80_000_000))
}

// TestB2 — user boleh MEMILIH menutup selisih lewat jurnal saldo awal, dengan
// akun lawan yang dia pilih sendiri dari COA. Tidak ada akun clearing hardcode.
func TestB2_OpeningJournalUsesUserChosenCounter(t *testing.T) {
	e := larSetup(t)
	// Buku besar kosong: seluruh 100jt harus dibentuk oleh jurnal saldo awal.
	res := e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-B2", "100.000.000", "", "", "", ""},
	}, "3-1000")

	if res.OpeningJournal == nil {
		t.Fatal("OpeningJournal nil, want terbentuk")
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(100_000_000))
	mustEqual(t, "saldo 3-1000", e.accountBalance(t, "3-1000"), money(100_000_000))

	rec, err := e.svc.Reconcile(context.Background(), larTenant, "1-2000", larAsOf, domain.Zero)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if !rec.Matched {
		t.Errorf("setelah jurnal saldo awal harus cocok, selisih = %s", rec.Difference.String())
	}

	// Akun lawan harus benar-benar akun PILIHAN user, bukan yang ditebak sistem.
	var counterCount int64
	e.db.Raw(`SELECT COUNT(*) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		WHERE jl.journal_entry_id = ? AND a.code = '3-1000'`, *res.OpeningJournal).Scan(&counterCount)
	if counterCount != 1 {
		t.Errorf("jurnal saldo awal tidak memakai 3-1000 sebagai lawan")
	}
}

// TestB3 — saldo awal yang SALAH lalu dikoreksi harus tetap cocok.
//
// Buku besar ini append-only: satu-satunya cara sah membatalkan saldo awal yang
// kelebihan adalah jurnal pembalik. Kalau pembalik itu digolongkan sebagai
// mutasi operasi (source "reversal"), porsi saldo awal tetap tampak 200jt
// padahal nyatanya 100jt, dan rekonsiliasi melaporkan selisih 100jt yang tidak
// pernah bisa ditutup oleh apa pun. Pembalik harus mengurangi kelompok asalnya.
func TestB3_ReversedOpeningBalanceStaysMatched(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()

	e.seedOpening(t, 100_000_000)
	e.seedOpening(t, 100_000_000) // salah entri — dobel
	mustEqual(t, "saldo 1-2000 sebelum koreksi", e.accountBalance(t, "1-2000"), money(200_000_000))

	var dupID uint64
	if err := e.db.Raw(`SELECT id FROM journal_entries
		WHERE tenant_id = ? AND source = ? ORDER BY id DESC LIMIT 1`,
		larTenant, string(ledger.SourceOpeningBalance)).Scan(&dupID).Error; err != nil || dupID == 0 {
		t.Fatalf("jurnal saldo awal kedua tidak ditemukan: %v", err)
	}
	repo := ledger.NewGORMRepository(e.db)
	posting := ledger.NewPostingService(repo, repo).WithPeriodChecker(repo)
	if _, err := posting.Reverse(ctx, larTenant, dupID, larAsOf); err != nil {
		t.Fatalf("balikkan saldo awal dobel: %v", err)
	}
	mustEqual(t, "saldo 1-2000 sesudah koreksi", e.accountBalance(t, "1-2000"), money(100_000_000))

	res := e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-B3", "100.000.000", "", "", "", ""},
	}, "")

	rec := res.Reconciliation
	mustEqual(t, "OpeningPortion", rec.OpeningPortion, money(100_000_000))
	mustEqual(t, "OperationalMovement", rec.OperationalMovement, domain.Zero)
	mustEqual(t, "Difference", rec.Difference, domain.Zero)
	if !rec.Matched {
		t.Errorf("Matched = false setelah koreksi yang sah, selisih = %s", rec.Difference.String())
	}
}

// ═══ Skenario C & D & I — pelunasan ══════════════════════════════════════════
//
// C: Budi 100jt bayar 30jt → sisa 70jt, GL 1-2000 turun 30jt.
// D: lalu bayar 70jt → sisa 0, status paid.
// I: keduanya menerbitkan BKM (INV-DOC-1).
func TestC_D_I_PaymentReducesOutstandingAndLedger(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 100_000_000)
	e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-C1", "100.000.000", "", "", "", ""},
	}, "")

	rows := e.openReceivables(t)
	if len(rows) != 1 {
		t.Fatalf("piutang = %d, want 1", len(rows))
	}
	budi := rows[0].ID

	// ── C: bayar 30jt ────────────────────────────────────────────────────────
	pay1, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(30_000_000)}},
		CashAccountCode: "1-1100",
		PaymentDate:     time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("bayar 30jt: %v", err)
	}
	mustEqual(t, "total bayar", pay1.TotalAmount, money(30_000_000))

	// INV-DOC-1 — satu pergerakan kas, satu dokumen bernomor.
	if pay1.DocumentID == nil || pay1.DocumentNumber == "" {
		t.Fatalf("pelunasan tanpa dokumen: id=%v no=%q", pay1.DocumentID, pay1.DocumentNumber)
	}
	var docType string
	e.db.Raw(`SELECT document_type_code FROM documents WHERE id = ?`, *pay1.DocumentID).Scan(&docType)
	if docType != ledger.DocCashIn {
		t.Errorf("jenis dokumen = %q, want %q (BKM)", docType, ledger.DocCashIn)
	}

	detail, err := e.svc.GetReceivable(ctx, larTenant, budi)
	if err != nil {
		t.Fatalf("detail: %v", err)
	}
	mustEqual(t, "sisa setelah 30jt", detail.Outstanding, money(70_000_000))
	if detail.Receivable.Status != legacyar.StatusOpen {
		t.Errorf("status = %s, want open", detail.Receivable.Status)
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(70_000_000))
	mustEqual(t, "saldo 1-1100", e.accountBalance(t, "1-1100"), money(30_000_000))

	// Rekonsiliasi TETAP cocok setelah pembayaran: turunnya saldo buku besar
	// sudah dijelaskan barisnya sendiri. Rekonsiliasi yang rusak tiap kali ada
	// pembayaran akan segera diabaikan orang.
	rec, err := e.svc.Reconcile(ctx, larTenant, "1-2000", time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), domain.Zero)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	mustEqual(t, "LegacyPaymentEffect", rec.LegacyPaymentEffect, money(-30_000_000))
	if !rec.Matched {
		t.Errorf("rekonsiliasi pasca-bayar tidak cocok: selisih %s", rec.Difference.String())
	}

	// ── D: lunasi sisa 70jt ──────────────────────────────────────────────────
	if _, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(70_000_000)}},
		CashAccountCode: "1-1100",
	}); err != nil {
		t.Fatalf("bayar 70jt: %v", err)
	}
	detail, _ = e.svc.GetReceivable(ctx, larTenant, budi)
	mustEqual(t, "sisa setelah lunas", detail.Outstanding, domain.Zero)
	if detail.Receivable.Status != legacyar.StatusPaid {
		t.Errorf("status = %s, want paid", detail.Receivable.Status)
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), domain.Zero)

	// INV-LAR-3 — cache paid_amount == Σ sub-ledger.
	var sum domain.Money
	e.db.Raw(`SELECT COALESCE(SUM(amount),0) FROM legacy_receivable_payments
		WHERE tenant_id = ? AND legacy_receivable_id = ?`, larTenant, budi).Row().Scan(&sum)
	mustEqual(t, "cache paid_amount", detail.Receivable.PaidAmount, sum)
}

// ═══ Skenario E — lebih bayar DITOLAK ════════════════════════════════════════
func TestE_OverpaymentRejected(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 100_000_000)
	e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-E1", "100.000.000", "", "", "", ""},
	}, "")
	budi := e.openReceivables(t)[0].ID

	_, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(150_000_000)}},
		CashAccountCode: "1-1100",
	})
	if !errors.Is(err, legacyar.ErrOverpayment) {
		t.Fatalf("err = %v, want ErrOverpayment", err)
	}

	// Penolakan harus BERSIH: tidak ada jurnal, dokumen, maupun baris pelunasan
	// yang tertinggal. Transaksi yang gagal separuh jalan justru lebih buruk
	// daripada pembayaran yang ditolak.
	var n int64
	e.db.Raw(`SELECT COUNT(*) FROM legacy_receivable_payments WHERE tenant_id = ?`, larTenant).Scan(&n)
	if n != 0 {
		t.Errorf("ada %d baris pelunasan tertinggal", n)
	}
	mustEqual(t, "saldo 1-1100", e.accountBalance(t, "1-1100"), domain.Zero)
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(100_000_000))
}

// TestE2 — lebih bayar juga ditolak ketika sudah ada cicilan sebelumnya.
func TestE2_OverpaymentAfterPartial(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 100_000_000)
	e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-E2", "100.000.000", "", "", "", ""},
	}, "")
	budi := e.openReceivables(t)[0].ID

	if _, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(60_000_000)}},
		CashAccountCode: "1-1100",
	}); err != nil {
		t.Fatalf("bayar 60jt: %v", err)
	}
	_, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(41_000_000)}},
		CashAccountCode: "1-1100",
	})
	if !errors.Is(err, legacyar.ErrOverpayment) {
		t.Fatalf("err = %v, want ErrOverpayment (sisa 40jt, bayar 41jt)", err)
	}
}

// ═══ Skenario F — batch yang sama dua kali DITOLAK ═══════════════════════════
func TestF_DuplicateBatchRejected(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 1_000_000_000)
	content := buildFile(t, threeCustomers())

	b1, err := e.svc.Upload(ctx, larTenant, legacyar.UploadRequest{
		FileName: "piutang.xlsx", Content: content, AsOfDate: larAsOf,
	})
	if err != nil {
		t.Fatalf("upload 1: %v", err)
	}
	if _, err := e.svc.Commit(ctx, larTenant, b1.ID, legacyar.CommitRequest{}); err != nil {
		t.Fatalf("commit 1: %v", err)
	}

	// Lapis 1 — berkas identik ditolak di gerbang unggah.
	if _, err := e.svc.Upload(ctx, larTenant, legacyar.UploadRequest{
		FileName: "piutang.xlsx", Content: content, AsOfDate: larAsOf,
	}); !errors.Is(err, legacyar.ErrBatchDuplicate) {
		t.Fatalf("upload ulang err = %v, want ErrBatchDuplicate", err)
	}

	// Lapis 2 — berkas BERBEDA (nama customer diubah) sehingga hash-nya lolos,
	// tapi nomor rujukannya sudah dipakai. Di sini penolakannya sengaja PER
	// BARIS, bukan 409 selembar: admin yang menambahkan lima piutang baru ke
	// berkas lama perlu tahu baris mana yang bentrok, bukan cuma bahwa ada
	// yang bentrok. Gerbangnya tetap tertutup di commit.
	altered := threeCustomers()
	altered[0][0] = "Budi S."
	b2, err := e.svc.Upload(ctx, larTenant, legacyar.UploadRequest{
		FileName: "piutang-v2.xlsx", Content: buildFile(t, altered), AsOfDate: larAsOf,
	})
	if err != nil {
		t.Fatalf("upload v2: %v", err)
	}
	if b2.ErrorCount != 3 {
		t.Errorf("ErrorCount = %d, want 3 (ketiga rujukan sudah dipakai)", b2.ErrorCount)
	}
	if _, err := e.svc.Commit(ctx, larTenant, b2.ID, legacyar.CommitRequest{}); !errors.Is(err, legacyar.ErrHasErrorRows) {
		t.Fatalf("commit v2 err = %v, want ErrHasErrorRows", err)
	}

	// Tidak ada yang tergandakan.
	var n int64
	e.db.Raw(`SELECT COUNT(*) FROM legacy_receivables WHERE tenant_id = ?`, larTenant).Scan(&n)
	if n != 3 {
		t.Errorf("piutang = %d, want 3", n)
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(1_000_000_000))
}

// TestF2 — commit dua kali atas batch yang sama ditolak.
func TestF2_DoubleCommitRejected(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 100_000_000)
	b, err := e.svc.Upload(ctx, larTenant, legacyar.UploadRequest{
		FileName: "x.xlsx", AsOfDate: larAsOf,
		Content: buildFile(t, [][]string{
			{"Budi Santoso", "Griya Asri", "REF-F2", "100.000.000", "", "", "", ""},
		}),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if _, err := e.svc.Commit(ctx, larTenant, b.ID, legacyar.CommitRequest{}); err != nil {
		t.Fatalf("commit 1: %v", err)
	}
	if _, err := e.svc.Commit(ctx, larTenant, b.ID, legacyar.CommitRequest{}); !errors.Is(err, legacyar.ErrBatchNotDraft) {
		t.Fatalf("commit 2 err = %v, want ErrBatchNotDraft", err)
	}
	var n int64
	e.db.Raw(`SELECT COUNT(*) FROM legacy_receivables WHERE tenant_id = ?`, larTenant).Scan(&n)
	if n != 1 {
		t.Errorf("piutang = %d, want 1", n)
	}
}

// ═══ Skenario G — alokasi dua customer tidak tertukar ════════════════════════
func TestG_AllocationNotSwapped(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 1_000_000_000)
	e.importFile(t, threeCustomers(), "")

	byName := map[string]uint64{}
	for _, r := range e.openReceivables(t) {
		byName[r.CustomerName] = r.ID
	}
	budi, andi := byName["Budi Santoso"], byName["Andi Wijaya"]
	if budi == 0 || andi == 0 {
		t.Fatalf("piutang tidak lengkap: %+v", byName)
	}

	// Satu setoran 150jt yang menutup dua piutang berbeda dengan porsi berbeda.
	res, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations: []legacyar.PaymentAllocation{
			{ReceivableID: budi, Amount: money(100_000_000)},
			{ReceivableID: andi, Amount: money(50_000_000)},
		},
		CashAccountCode: "1-1100",
	})
	if err != nil {
		t.Fatalf("bayar gabungan: %v", err)
	}
	mustEqual(t, "total", res.TotalAmount, money(150_000_000))
	if len(res.Payments) != 2 {
		t.Fatalf("baris pelunasan = %d, want 2", len(res.Payments))
	}
	// Satu jurnal, satu dokumen — bukan dua kwitansi untuk satu setoran.
	if res.Payments[0].JournalEntryID != res.Payments[1].JournalEntryID {
		t.Error("satu setoran menghasilkan dua jurnal")
	}

	bd, _ := e.svc.GetReceivable(ctx, larTenant, budi)
	ad, _ := e.svc.GetReceivable(ctx, larTenant, andi)
	mustEqual(t, "sisa Budi", bd.Outstanding, money(200_000_000))
	mustEqual(t, "sisa Andi", ad.Outstanding, money(200_000_000))
	if bd.Receivable.CustomerName != "Budi Santoso" || ad.Receivable.CustomerName != "Andi Wijaya" {
		t.Fatal("identitas piutang tertukar")
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(850_000_000))
}

// ═══ Skenario H — legacy muncul di daftar piutang yang SAMA ══════════════════
//
// INV-AR-1: satu mesin aging, `legacy` hanyalah sumber ketiga. Kalau baris ini
// perlu halaman sendiri, W-4 sudah gagal.
func TestH_LegacyAppearsInOneReceivableReport(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 1_000_000_000)
	e.importFile(t, threeCustomers(), "")

	// Cutoff dipilih supaya ketiga piutang jatuh di bucket yang BERBEDA-beda:
	// kalau semuanya mendarat di ">90 hari", tes ini lulus tanpa membuktikan
	// bahwa umurnya benar-benar dihitung per baris.
	asOf := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	all, err := e.rep.GetARAging(ctx, larTenant, asOf, "")
	if err != nil {
		t.Fatalf("aging gabungan: %v", err)
	}
	legacyOnly, err := e.rep.GetARAging(ctx, larTenant, asOf, receivable.SourceLegacy)
	if err != nil {
		t.Fatalf("aging legacy: %v", err)
	}

	if len(legacyOnly.Rows) != 3 {
		t.Fatalf("baris legacy = %d, want 3", len(legacyOnly.Rows))
	}
	mustEqualStr(t, "total piutang legacy", legacyOnly.TotalPiutang, money(1_000_000_000))
	// Tanpa filter, ketiganya tetap muncul — sumbernya dimensi, bukan saringan.
	if len(all.Rows) < 3 {
		t.Fatalf("daftar gabungan = %d baris, want >= 3", len(all.Rows))
	}

	var found bool
	for _, s := range all.BySource {
		if s.Source == receivable.SourceLegacy {
			found = true
			if s.Count != 3 {
				t.Errorf("by_source legacy count = %d, want 3", s.Count)
			}
			mustEqualStr(t, "subtotal by_source legacy", s.Outstanding, money(1_000_000_000))
		}
	}
	if !found {
		t.Error("by_source tidak memuat `legacy` — layar akan kehilangan subtotalnya")
	}

	// Piutang lama tidak punya unit; keterangannya ada di Label, bukan UnitCode
	// (aturan kolom receivable.Row). Menjejalkannya ke UnitCode membuat kolom
	// yang sama berarti dua hal tergantung siapa yang mengisi.
	for _, r := range legacyOnly.Rows {
		if r.UnitCode != "" {
			t.Errorf("baris legacy mengisi unit_code = %q", r.UnitCode)
		}
		if r.Label == "" {
			t.Error("baris legacy tanpa label — penagih tidak tahu ini piutang apa")
		}
	}

	// Umur piutang dihitung mesin yang sama seperti piutang berjalan. Per
	// 31 Mei 2026: Budi (30 Jun 2025) & Andi (30 Sep 2025) menunggak >90 hari,
	// Citra (31 Mar 2026) baru 61 hari.
	buckets := map[receivable.Bucket]int{}
	for _, r := range legacyOnly.Rows {
		buckets[r.Bucket]++
	}
	if buckets[receivable.Bucket90Plus] != 2 || buckets[receivable.Bucket61_90] != 1 {
		t.Errorf("sebaran bucket = %v, want 2×90_plus + 1×61_90", buckets)
	}
}

// ═══ Void — pembalik, bukan penghapus ════════════════════════════════════════
//
// Invariant #5: jurnal terposting immutable. Pembatalan pelunasan menghasilkan
// jurnal pembalik dan baris cermin NEGATIF, bukan DELETE.
func TestVoidPaymentReverses(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 100_000_000)
	e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-V1", "100.000.000", "", "", "", ""},
	}, "")
	budi := e.openReceivables(t)[0].ID

	pay, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(30_000_000)}},
		CashAccountCode: "1-1100",
	})
	if err != nil {
		t.Fatalf("bayar: %v", err)
	}
	paymentID := pay.Payments[0].ID

	if _, err := e.svc.VoidPayment(ctx, larTenant, paymentID, "setoran salah orang", nil); err != nil {
		t.Fatalf("void: %v", err)
	}

	detail, _ := e.svc.GetReceivable(ctx, larTenant, budi)
	mustEqual(t, "sisa setelah void", detail.Outstanding, money(100_000_000))
	if detail.Receivable.Status != legacyar.StatusOpen {
		t.Errorf("status = %s, want open", detail.Receivable.Status)
	}
	mustEqual(t, "saldo 1-2000", e.accountBalance(t, "1-2000"), money(100_000_000))
	mustEqual(t, "saldo 1-1100", e.accountBalance(t, "1-1100"), domain.Zero)

	// Riwayatnya tetap terbaca: dua baris, bukan nol.
	if len(detail.Payments) != 2 {
		t.Fatalf("baris pelunasan = %d, want 2 (asli + cermin negatif)", len(detail.Payments))
	}
	// Jurnal aslinya tidak disentuh.
	var origLines int64
	e.db.Raw(`SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = ?`, pay.JournalEntryID).Scan(&origLines)
	if origLines != 2 {
		t.Errorf("jurnal asli berubah: %d baris", origLines)
	}

	// Void dua kali ditolak.
	if _, err := e.svc.VoidPayment(ctx, larTenant, paymentID, "coba lagi", nil); !errors.Is(err, legacyar.ErrPaymentVoided) {
		t.Errorf("void kedua err = %v, want ErrPaymentVoided", err)
	}
}

// ═══ Idempotensi pembayaran ══════════════════════════════════════════════════
//
// Klik ganda pada tombol bayar tidak boleh menghasilkan dua penerimaan kas.
func TestPaymentIdempotency(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 100_000_000)
	e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-ID1", "100.000.000", "", "", "", ""},
	}, "")
	budi := e.openReceivables(t)[0].ID

	req := legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(30_000_000)}},
		CashAccountCode: "1-1100",
		IdempotencyKey:  "klik-ganda-001",
	}
	first, err := e.svc.ReceivePayment(ctx, larTenant, req)
	if err != nil {
		t.Fatalf("bayar 1: %v", err)
	}
	second, err := e.svc.ReceivePayment(ctx, larTenant, req)
	if err != nil {
		t.Fatalf("bayar 2 (idempoten): %v", err)
	}
	if !second.Duplicate {
		t.Error("panggilan kedua tidak ditandai duplikat")
	}
	if second.JournalEntryID != first.JournalEntryID {
		t.Errorf("jurnal berbeda: %d vs %d", second.JournalEntryID, first.JournalEntryID)
	}
	detail, _ := e.svc.GetReceivable(ctx, larTenant, budi)
	mustEqual(t, "sisa", detail.Outstanding, money(70_000_000))
	mustEqual(t, "saldo 1-1100", e.accountBalance(t, "1-1100"), money(30_000_000))
}

// ═══ Isolasi tenant ══════════════════════════════════════════════════════════
func TestTenantIsolation(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 1_000_000_000)
	e.importFile(t, threeCustomers(), "")

	const other uint64 = larTenant + 1
	e.db.Exec("DELETE FROM legacy_receivables WHERE tenant_id = ?", other)

	rows, err := e.svc.ListReceivables(ctx, other, legacyar.ListFilter{})
	if err != nil {
		t.Fatalf("list tenant lain: %v", err)
	}
	if len(rows.Rows) != 0 {
		t.Errorf("tenant lain melihat %d piutang, want 0", len(rows.Rows))
	}
	sum, err := e.svc.SumOutstanding(ctx, other)
	if err != nil {
		t.Fatalf("sum tenant lain: %v", err)
	}
	mustEqual(t, "sum tenant lain", sum, domain.Zero)

	budi := e.openReceivables(t)[0].ID
	if _, err := e.svc.ReceivePayment(ctx, other, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(1_000_000)}},
		CashAccountCode: "1-1100",
	}); err == nil {
		t.Error("tenant lain berhasil membayar piutang tenant ini")
	}
}

// ═══ Berkas dengan baris error tidak boleh diimpor sebagian ══════════════════
func TestNoPartialImport(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	b, err := e.svc.Upload(ctx, larTenant, legacyar.UploadRequest{
		FileName: "campur.xlsx", AsOfDate: larAsOf,
		Content: buildFile(t, [][]string{
			{"Budi Santoso", "Griya Asri", "REF-P1", "100.000.000", "", "", "", ""},
			{"", "Griya Asri", "REF-P2", "50.000.000", "", "", "", ""}, // nama kosong → error
		}),
	})
	if err != nil {
		t.Fatalf("upload: %v", err)
	}
	if b.ErrorCount != 1 {
		t.Fatalf("ErrorCount = %d, want 1", b.ErrorCount)
	}
	if _, err := e.svc.Commit(ctx, larTenant, b.ID, legacyar.CommitRequest{}); !errors.Is(err, legacyar.ErrHasErrorRows) {
		t.Fatalf("commit err = %v, want ErrHasErrorRows", err)
	}
	var n int64
	e.db.Raw(`SELECT COUNT(*) FROM legacy_receivables WHERE tenant_id = ?`, larTenant).Scan(&n)
	if n != 0 {
		t.Errorf("impor sebagian terjadi: %d baris masuk", n)
	}
}

// ═══ Akun kas harus benar-benar kas/bank ═════════════════════════════════════
func TestCashAccountMustBeCashOrBank(t *testing.T) {
	e := larSetup(t)
	ctx := context.Background()
	e.seedOpening(t, 100_000_000)
	e.importFile(t, [][]string{
		{"Budi Santoso", "Griya Asri", "REF-K1", "100.000.000", "", "", "", ""},
	}, "")
	budi := e.openReceivables(t)[0].ID

	// 5-1000 adalah akun beban — uang tidak masuk ke sana.
	_, err := e.svc.ReceivePayment(ctx, larTenant, legacyar.PaymentRequest{
		Allocations:     []legacyar.PaymentAllocation{{ReceivableID: budi, Amount: money(1_000_000)}},
		CashAccountCode: "5-1000",
	})
	if !errors.Is(err, legacyar.ErrCashAccount) {
		t.Fatalf("err = %v, want ErrCashAccount", err)
	}
}
