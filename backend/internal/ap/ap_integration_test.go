//go:build integration

package ap_test

// W-11 Tahap 2 — Hutang Usaha (vendor + tagihan) terhadap MySQL nyata.
//
// Yang dibuktikan di sini adalah hal-hal yang tidak bisa dibuktikan unit test:
// tagihan draft benar-benar TIDAK hidup di buku besar maupun di realisasi RAB,
// baris biayanya benar-benar mendarat di `cost_entries` yang dibaca modul RAB
// yang sudah ada, PPN benar-benar tidak ikut terhitung sebagai realisasi, gate
// persetujuan benar-benar opt-in, dan penolakan tidak meninggalkan setengah
// transaksi.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (>= 000073).

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/ap"
	"esaproperti/internal/approval"
	"esaproperti/internal/budget"
	"esaproperti/internal/cost"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
)

const (
	w11Tenant      uint64 = 9_900_074
	w11OtherTenant uint64 = 9_900_075
)

// ── Fixture ─────────────────────────────────────────────────────────────────

type w11Fixture struct {
	db  *gorm.DB
	svc *ap.Service

	projectID uint64
	unitID    uint64
	planID    uint64
	itemID    uint64

	vendorPKP    uint64
	vendorNonPKP uint64
}

func w11Connect(t *testing.T) *gorm.DB {
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

// w11Cleanup menghapus HANYA baris milik tenant uji. Urutan mengikuti arah
// foreign key: anak dulu, induk belakangan.
func w11Cleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	tables := []string{
		"ap_payment_allocations", "ap_payments", "cost_entries", "ap_invoices", "vendors",
		"documents", "document_sequences",
		"journal_lines", "journal_entries", "accounts",
		"approval_actions", "approval_requests", "approval_steps", "approval_workflows",
		"budget_items", "budget_plans", "units", "projects", "product_types",
		"expense_types", "accounting_periods",
	}
	for _, tn := range tables {
		for _, tid := range []uint64{w11Tenant, w11OtherTenant} {
			if err := db.Exec("DELETE FROM "+tn+" WHERE tenant_id = ?", tid).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tn, err)
			}
		}
	}
}

// w11Service merakit service AP dengan planner biaya yang SAMA seperti produksi.
// Merakitnya dengan resolver yang lebih sedikit akan membuat test membuktikan
// aturan yang tidak berlaku di produksi.
func w11Service(t *testing.T, db *gorm.DB) *ap.Service {
	t.Helper()
	repo := cost.NewGORMRepository(db)
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	planner := cost.NewService(repo, cost.NewLedgerJournalAdapter(posting), repo, repo, &w11BudgetLookup{repo: budget.NewGORMRepository(db)})
	planner.SetUnitPolicyResolver(project.NewGORMRepository(db))
	planner.SetExpenseTypeResolver(repo)
	planner.SetPaymentAccountValidator(repo)
	planner.SetUnitProjectResolver(repo)
	return ap.NewService(db, planner)
}

type w11BudgetLookup struct{ repo *budget.GORMRepository }

func (a *w11BudgetLookup) FindItemForCostValidation(
	ctx context.Context, tenantID, projectID, itemID uint64,
) (domain.CostCategory, error) {
	foundProjectID, cc, isMappable, err := a.repo.FindItemForCostLookup(ctx, tenantID, itemID)
	if errors.Is(err, budget.ErrItemNotFound) {
		return "", cost.ErrBudgetItemNotFound
	}
	if err != nil {
		return "", err
	}
	if foundProjectID != projectID {
		return "", cost.ErrBudgetItemProjectMismatch
	}
	if !isMappable {
		return "", cost.ErrBudgetItemCategoryMismatch
	}
	return cc, nil
}

func w11Setup(t *testing.T) *w11Fixture {
	t.Helper()
	db := w11Connect(t)
	w11Cleanup(t, db)
	t.Cleanup(func() { w11Cleanup(t, db) })

	ctx := context.Background()
	for _, tid := range []uint64{w11Tenant, w11OtherTenant} {
		if err := ledger.SeedCOA(ctx, db, tid); err != nil {
			t.Fatalf("seed COA: %v", err)
		}
		if err := project.SeedDefaultProductTypes(ctx, db, tid); err != nil {
			t.Fatalf("seed product types: %v", err)
		}
		// Master jenis dokumen. Tanpa ini pembayaran tidak bisa menerbitkan BKK,
		// dan karena INV-DOC-1 fail-closed, kas tidak akan bergerak sama sekali.
		if err := document.SeedDefaultDocumentTypes(ctx, db, tid); err != nil {
			t.Fatalf("seed jenis dokumen: %v", err)
		}
	}

	f := &w11Fixture{db: db, svc: w11Service(t, db)}

	p := &project.Project{TenantID: w11Tenant, Name: "Proyek W11", Status: project.ProjectStatusActive}
	if err := db.Create(p).Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	f.projectID = p.ID

	u := &project.Unit{TenantID: w11Tenant, ProjectID: p.ID, Code: "W11-A01", UnitType: "rumah", Status: project.UnitStatusAvailable}
	if err := db.Create(u).Error; err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	f.unitID = u.ID

	budgeted, _ := domain.NewMoney("500000000")
	plan := &budget.BudgetPlan{TenantID: w11Tenant, ProjectID: p.ID, Version: 1, Label: "RAB W11", Status: budget.BudgetPlanStatusActive}
	if err := db.Create(plan).Error; err != nil {
		t.Fatalf("seed budget plan: %v", err)
	}
	item := &budget.BudgetItem{TenantID: w11Tenant, BudgetPlanID: plan.ID, Category: budget.BudgetCategoryConstruction, Subcategory: "Struktur", BudgetedAmount: budgeted}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("seed budget item: %v", err)
	}
	f.planID, f.itemID = plan.ID, item.ID

	pkp, err := f.svc.CreateVendor(ctx, w11Tenant, ap.CreateVendorRequest{
		Name: "PT Kontraktor Jaya", NPWP: "01.234.567.8-901.000", IsPKP: true,
	})
	if err != nil {
		t.Fatalf("buat vendor PKP: %v", err)
	}
	nonPKP, err := f.svc.CreateVendor(ctx, w11Tenant, ap.CreateVendorRequest{
		Name: "CV Tukang Mandiri", IsPKP: false,
	})
	if err != nil {
		t.Fatalf("buat vendor non-PKP: %v", err)
	}
	f.vendorPKP, f.vendorNonPKP = pkp.ID, nonPKP.ID

	return f
}

// ── Helper ──────────────────────────────────────────────────────────────────

func w11Money(t *testing.T, s string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(s)
	if err != nil {
		t.Fatalf("nominal %q: %v", s, err)
	}
	return m
}

// tagihanProyek membangun tagihan konstruksi bertaut item RAB.
func (f *w11Fixture) tagihanProyek(t *testing.T, no, amount string) ap.CreateInvoiceRequest {
	t.Helper()
	item := f.itemID
	return ap.CreateInvoiceRequest{
		VendorID:      f.vendorPKP,
		ProjectID:     f.projectID,
		InvoiceNumber: no,
		InvoiceDate:   time.Now(),
		DueDate:       time.Now().AddDate(0, 1, 0),
		Description:   "Termin pekerjaan struktur",
		Lines: []ap.InvoiceLineInput{{
			Category:     domain.CostCategoryHard,
			CostTier:     domain.CostTierShared,
			Amount:       w11Money(t, amount),
			BudgetItemID: &item,
			Description:  "Struktur lantai 1",
		}},
	}
}

// realisasiItem membaca realisasi per item RAB lewat pembaca yang SUDAH ADA —
// bukan lewat query yang ditulis khusus untuk test ini. Kalau AP tidak terbaca
// oleh pembaca produksi, test ini yang harus gagal.
func (f *w11Fixture) realisasiItem(t *testing.T) domain.Money {
	t.Helper()
	m, err := budget.NewGORMRealisasiProvider(f.db).GetRealisasiPerItem(context.Background(), w11Tenant, f.planID)
	if err != nil {
		t.Fatalf("baca realisasi RAB: %v", err)
	}
	v, ok := m[f.itemID]
	if !ok {
		return domain.Zero
	}
	return v
}

// saldoAkun menghitung saldo (debit − kredit) sebuah akun dari jurnal TERPOSTING.
func (f *w11Fixture) saldoAkun(t *testing.T, code string) domain.Money {
	t.Helper()
	var raw *string
	err := f.db.Raw(`
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0)
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = ? AND a.code = ? AND je.posted_at IS NOT NULL`,
		w11Tenant, code).Scan(&raw).Error
	if err != nil {
		t.Fatalf("baca saldo %s: %v", code, err)
	}
	if raw == nil {
		return domain.Zero
	}
	return w11Money(t, *raw)
}

func (f *w11Fixture) countInvoices(t *testing.T, tenantID uint64) int64 {
	t.Helper()
	var n int64
	if err := f.db.Raw("SELECT COUNT(*) FROM ap_invoices WHERE tenant_id = ?", tenantID).Scan(&n).Error; err != nil {
		t.Fatalf("hitung tagihan: %v", err)
	}
	return n
}

// ── 1. Alur utuh: vendor → draft → post → cost_entries → jurnal → RAB ───────

func TestAPAlurUtuh(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	// Pratinjau dulu — angkanya harus sama persis dengan yang akhirnya terbit.
	prev, err := f.svc.PreviewInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/001", "100000000"))
	if err != nil {
		t.Fatalf("pratinjau: %v", err)
	}
	if prev.PayableAmount != "100000000" {
		t.Fatalf("pratinjau kewajiban = %s, mau 100000000", prev.PayableAmount)
	}
	if f.countInvoices(t, w11Tenant) != 0 {
		t.Fatal("pratinjau menulis tagihan ke DB")
	}

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/001", "100000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}
	if inv.Status != ap.InvoiceDraft {
		t.Fatalf("status awal = %s, mau draft", inv.Status)
	}

	// INV-AP-9: draft tidak hidup di mana pun.
	if s := f.saldoAkun(t, "2-1000"); !s.IsZero() {
		t.Fatalf("draft sudah menambah hutang usaha: %s", s)
	}
	if r := f.realisasiItem(t); !r.IsZero() {
		t.Fatalf("draft sudah menambah realisasi RAB: %s", r)
	}
	view, err := f.svc.GetInvoice(ctx, w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("baca tagihan: %v", err)
	}
	if view.PaymentStatus != ap.PayStatusUnrecognized {
		t.Fatalf("status pembayaran draft = %s, mau unrecognized", view.PaymentStatus)
	}
	if len(view.Lines) != 1 {
		t.Fatalf("baris biaya tersimpan = %d, mau 1", len(view.Lines))
	}
	if view.Lines[0].PaymentMethod != cost.PaymentMethodPayable {
		t.Fatalf("metode pembayaran baris = %s, mau payable", view.Lines[0].PaymentMethod)
	}

	posted, err := f.svc.PostInvoice(ctx, w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("posting tagihan: %v", err)
	}
	if posted.Status != ap.InvoicePosted || posted.PostedAt == nil {
		t.Fatalf("status setelah posting = %s (posted_at %v)", posted.Status, posted.PostedAt)
	}

	// Kewajiban lahir, biaya terkapitalisasi, RAB terealisasi — ketiganya
	// dengan angka yang sama.
	if got, want := f.saldoAkun(t, "2-1000").String(), "-100000000"; got != want {
		t.Fatalf("saldo hutang usaha = %s, mau %s (kredit)", got, want)
	}
	if got, want := f.saldoAkun(t, "1-3100").String(), "100000000"; got != want {
		t.Fatalf("saldo persediaan hard cost = %s, mau %s", got, want)
	}
	if got, want := f.realisasiItem(t).String(), "100000000"; got != want {
		t.Fatalf("realisasi RAB = %s, mau %s", got, want)
	}

	// Posting ulang ditolak.
	if _, err := f.svc.PostInvoice(ctx, w11Tenant, inv.ID); !errors.Is(err, ap.ErrInvoiceNotDraft) {
		t.Fatalf("posting kedua: error = %v, mau %v", err, ap.ErrInvoiceNotDraft)
	}
}

// ── 2. PPN Masukan: menambah kewajiban, TIDAK menambah realisasi RAB ────────

func TestAPPPNTidakMenambahRealisasiRAB(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	req := f.tagihanProyek(t, "INV/W11/PPN", "100000000")
	req.PPNAmount = w11Money(t, "11000000")
	req.FakturPajakNumber = "010.000-25.00000001"

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, req)
	if err != nil {
		t.Fatalf("catat tagihan PPN: %v", err)
	}
	if _, err := f.svc.PostInvoice(ctx, w11Tenant, inv.ID); err != nil {
		t.Fatalf("posting: %v", err)
	}

	if got, want := f.saldoAkun(t, "2-1000").String(), "-111000000"; got != want {
		t.Fatalf("hutang usaha = %s, mau %s (DPP + PPN)", got, want)
	}
	if got, want := f.saldoAkun(t, "1-5100").String(), "11000000"; got != want {
		t.Fatalf("PPN Masukan = %s, mau %s", got, want)
	}
	// Inti aturannya: RAB terealisasi sebesar DPP saja.
	if got, want := f.realisasiItem(t).String(), "100000000"; got != want {
		t.Fatalf("realisasi RAB = %s, mau %s (PPN tidak boleh ikut)", got, want)
	}

	// INV-AP-17 dibuktikan pada baris jurnal yang benar-benar tersimpan.
	var tagged int64
	if err := f.db.Raw(`
		SELECT COUNT(*) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = ? AND a.code = '1-5100'
		  AND (jl.project_id IS NOT NULL OR jl.unit_id IS NOT NULL OR jl.phase_id IS NOT NULL)`,
		w11Tenant).Scan(&tagged).Error; err != nil {
		t.Fatalf("periksa tag baris PPN: %v", err)
	}
	if tagged != 0 {
		t.Fatalf("%d baris PPN Masukan diberi tag proyek/unit/fase", tagged)
	}

	// Dan `cost_entries` tetap mencatat DPP, bukan nilai bruto tagihan.
	view, err := f.svc.GetInvoice(ctx, w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("baca tagihan: %v", err)
	}
	if got := view.Lines[0].Amount.String(); got != "100000000" {
		t.Fatalf("cost_entries.amount = %s, mau DPP 100000000", got)
	}
}

func TestAPVendorNonPKPTolakPPN(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	req := f.tagihanProyek(t, "INV/W11/NONPKP", "50000000")
	req.VendorID = f.vendorNonPKP
	req.PPNAmount = w11Money(t, "5500000")
	req.FakturPajakNumber = "010.000-25.00000002"

	if _, err := f.svc.RecordInvoice(ctx, w11Tenant, req); !errors.Is(err, ap.ErrPPNRequiresPKP) {
		t.Fatalf("error = %v, mau %v", err, ap.ErrPPNRequiresPKP)
	}
	if n := f.countInvoices(t, w11Tenant); n != 0 {
		t.Fatalf("%d tagihan tertulis padahal ditolak", n)
	}

	// PPN dari vendor PKP tanpa nomor faktur juga ditolak.
	req2 := f.tagihanProyek(t, "INV/W11/NOFAKTUR", "50000000")
	req2.PPNAmount = w11Money(t, "5500000")
	if _, err := f.svc.RecordInvoice(ctx, w11Tenant, req2); !errors.Is(err, ap.ErrFakturRequired) {
		t.Fatalf("error = %v, mau %v", err, ap.ErrFakturRequired)
	}
}

// ── 3. Isolasi tenant ───────────────────────────────────────────────────────

func TestAPIsolasiTenant(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/ISO", "10000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}

	if _, err := f.svc.GetInvoice(ctx, w11OtherTenant, inv.ID); !errors.Is(err, ap.ErrInvoiceNotFound) {
		t.Fatalf("baca lintas tenant: error = %v, mau %v", err, ap.ErrInvoiceNotFound)
	}
	if _, err := f.svc.PostInvoice(ctx, w11OtherTenant, inv.ID); !errors.Is(err, ap.ErrInvoiceNotFound) {
		t.Fatalf("posting lintas tenant: error = %v, mau %v", err, ap.ErrInvoiceNotFound)
	}
	if _, err := f.svc.GetVendor(ctx, w11OtherTenant, f.vendorPKP); !errors.Is(err, ap.ErrVendorNotFound) {
		t.Fatalf("baca vendor lintas tenant: error = %v, mau %v", err, ap.ErrVendorNotFound)
	}

	list, err := f.svc.ListInvoices(ctx, w11OtherTenant, ap.InvoiceFilter{})
	if err != nil {
		t.Fatalf("daftar tagihan tenant lain: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("tenant lain melihat %d tagihan", len(list))
	}
	vendors, err := f.svc.ListVendors(ctx, w11OtherTenant, ap.VendorFilter{})
	if err != nil {
		t.Fatalf("daftar vendor tenant lain: %v", err)
	}
	if len(vendors) != 0 {
		t.Fatalf("tenant lain melihat %d vendor", len(vendors))
	}

	// Tagihan tetap draft: penolakan di atas tidak boleh menghidupkannya.
	after, err := f.svc.GetInvoice(ctx, w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("baca ulang: %v", err)
	}
	if after.Status != ap.InvoiceDraft {
		t.Fatalf("status = %s, mau tetap draft", after.Status)
	}
}

// ── 4. Periode tertutup (D-9) ───────────────────────────────────────────────

func TestAPPeriodeTertutupSaatPosting(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/CLOSE", "20000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}

	// Periode ditutup SETELAH draft dibuat — inilah alasan periode diperiksa
	// lagi saat posting, bukan hanya saat pembuatan.
	now := time.Now()
	closed := &ledger.AccountingPeriod{
		TenantID: w11Tenant, Year: now.Year(), Month: int(now.Month()), Status: "closed", ClosedAt: &now,
	}
	if err := f.db.Create(closed).Error; err != nil {
		t.Fatalf("tutup periode: %v", err)
	}

	if _, err := f.svc.PostInvoice(ctx, w11Tenant, inv.ID); !errors.Is(err, ap.ErrPeriodClosed) {
		t.Fatalf("error = %v, mau %v", err, ap.ErrPeriodClosed)
	}
	if s := f.saldoAkun(t, "2-1000"); !s.IsZero() {
		t.Fatalf("hutang usaha terbentuk padahal posting gagal: %s", s)
	}
	after, err := f.svc.GetInvoice(ctx, w11Tenant, inv.ID)
	if err != nil {
		t.Fatalf("baca ulang: %v", err)
	}
	if after.Status != ap.InvoiceDraft {
		t.Fatalf("status = %s, mau tetap draft", after.Status)
	}
}

// ── 5. Pembalikan ───────────────────────────────────────────────────────────

func TestAPReverse(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/REV", "30000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}

	// Draft belum bisa dibalik — belum ada yang perlu dibatalkan.
	if _, err := f.svc.ReverseInvoice(ctx, w11Tenant, inv.ID, time.Time{}); !errors.Is(err, ap.ErrInvoiceNotPosted) {
		t.Fatalf("balik draft: error = %v, mau %v", err, ap.ErrInvoiceNotPosted)
	}

	if _, err := f.svc.PostInvoice(ctx, w11Tenant, inv.ID); err != nil {
		t.Fatalf("posting: %v", err)
	}
	rev, err := f.svc.ReverseInvoice(ctx, w11Tenant, inv.ID, time.Now())
	if err != nil {
		t.Fatalf("balik tagihan: %v", err)
	}
	if rev.Status != ap.InvoiceReversed || rev.ReversalJournalEntryID == nil {
		t.Fatalf("status = %s, jurnal pembalik = %v", rev.Status, rev.ReversalJournalEntryID)
	}

	// Invariant #5: jurnal asli TETAP ada dan tetap posted — koreksi lewat
	// pembalik, bukan penghapusan.
	var original ledger.JournalEntry
	if err := f.db.Where("tenant_id = ? AND id = ?", w11Tenant, inv.JournalEntryID).First(&original).Error; err != nil {
		t.Fatalf("jurnal asli hilang: %v", err)
	}
	if original.PostedAt == nil {
		t.Fatal("jurnal asli tidak lagi posted setelah dibalik")
	}

	// Buku besar bersih kembali.
	if s := f.saldoAkun(t, "2-1000"); !s.IsZero() {
		t.Fatalf("hutang usaha setelah pembalikan = %s, mau nol", s)
	}
	if s := f.saldoAkun(t, "1-3100"); !s.IsZero() {
		t.Fatalf("persediaan setelah pembalikan = %s, mau nol", s)
	}

	// ASIMETRI W-11b — DIDOKUMENTASIKAN, BUKAN DITAMBAL. Realisasi per item RAB
	// membaca Σ cost_entries atas jurnal yang posted; jurnal asli masih posted,
	// jadi angkanya TIDAK ikut turun. Test ini mengunci perilaku yang diketahui
	// supaya perubahannya kelak disengaja, bukan tak sengaja.
	if got, want := f.realisasiItem(t).String(), "30000000"; got != want {
		t.Fatalf("realisasi per item RAB = %s, mau %s (asimetri W-11b)", got, want)
	}

	if _, err := f.svc.ReverseInvoice(ctx, w11Tenant, inv.ID, time.Now()); !errors.Is(err, ap.ErrInvoiceReversed) {
		t.Fatalf("pembalikan kedua: error = %v, mau %v", err, ap.ErrInvoiceReversed)
	}
}

// ── 6. Gate persetujuan (D-16/D-20) — OPT-IN ────────────────────────────────

func TestAPGatePersetujuanOptIn(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()
	apprSvc := approval.NewService(approval.NewGORMRepository(f.db))
	gated := f.svc.WithApprovalGate(&w11Gate{svc: apprSvc})

	// Tanpa workflow aktif: gate tidak menghalangi apa pun.
	tanpaWF, err := gated.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/GATE-OFF", "10000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}
	if _, err := gated.PostInvoice(ctx, w11Tenant, tanpaWF.ID); err != nil {
		t.Fatalf("posting tanpa workflow harus lolos, tapi: %v", err)
	}

	// Dengan workflow aktif: posting tanpa persetujuan ditolak.
	if _, err := apprSvc.CreateWorkflow(ctx, w11Tenant, approval.CreateWorkflowRequest{
		TargetType: approval.TargetCost,
		Name:       "Persetujuan tagihan vendor",
		Steps:      []approval.StepInput{{Seq: 1, Name: "Owner", ApproverRole: "owner"}},
	}); err != nil {
		t.Fatalf("buat workflow: %v", err)
	}

	denganWF, err := gated.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/GATE-ON", "10000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}
	if _, err := gated.PostInvoice(ctx, w11Tenant, denganWF.ID); !errors.Is(err, ap.ErrApprovalRequired) {
		t.Fatalf("error = %v, mau %v", err, ap.ErrApprovalRequired)
	}

	// Setelah disetujui, posting lolos.
	req, err := apprSvc.SubmitRequest(ctx, w11Tenant, approval.SubmitRequestInput{
		TargetType: approval.TargetCost,
		TargetID:   denganWF.ID,
		Amount:     denganWF.PayableAmount,
	})
	if err != nil {
		t.Fatalf("ajukan persetujuan: %v", err)
	}
	if _, err := apprSvc.Act(ctx, w11Tenant, approval.ActInput{
		RequestID: req.ID, ActorID: 1, ActorRole: "owner", Decision: approval.DecisionApprove,
	}); err != nil {
		t.Fatalf("setujui: %v", err)
	}
	if _, err := gated.PostInvoice(ctx, w11Tenant, denganWF.ID); err != nil {
		t.Fatalf("posting setelah disetujui: %v", err)
	}
}

type w11Gate struct{ svc *approval.Service }

func (g *w11Gate) RequireApproved(ctx context.Context, tenantID, invoiceID uint64) error {
	err := g.svc.RequireApproved(ctx, tenantID, approval.TargetCost, invoiceID)
	if errors.Is(err, approval.ErrApprovalRequired) {
		return ap.ErrApprovalRequired
	}
	return err
}

// ── 7. Tagihan tanpa proyek (overhead) ──────────────────────────────────────

func TestAPTagihanOverheadTanpaProyek(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, ap.CreateInvoiceRequest{
		VendorID:      f.vendorPKP,
		InvoiceNumber: "INV/W11/OH",
		InvoiceDate:   time.Now(),
		DueDate:       time.Now().AddDate(0, 0, 30),
		Description:   "Sewa kantor pemasaran",
		Lines: []ap.InvoiceLineInput{{
			Category:    domain.CostCategoryOther,
			CostTier:    domain.CostTierOverhead,
			Amount:      w11Money(t, "5000000"),
			Description: "Sewa bulan berjalan",
		}},
	})
	if err != nil {
		t.Fatalf("catat tagihan overhead: %v", err)
	}
	if inv.ProjectID != nil {
		t.Fatalf("tagihan overhead punya project_id = %v, mau NULL", *inv.ProjectID)
	}
	if _, err := f.svc.PostInvoice(ctx, w11Tenant, inv.ID); err != nil {
		t.Fatalf("posting: %v", err)
	}
	if got, want := f.saldoAkun(t, "2-1000").String(), "-5000000"; got != want {
		t.Fatalf("hutang usaha = %s, mau %s", got, want)
	}
	// Overhead tidak tertaut item RAB proyek mana pun.
	if r := f.realisasiItem(t); !r.IsZero() {
		t.Fatalf("overhead menambah realisasi item RAB: %s", r)
	}
}

// ── 8. Baris biaya tidak sah ditolak lewat cost.PlanAPLine (A-1) ────────────

func TestAPBarisTidakSahDitolakFailClosed(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	cases := []struct {
		name   string
		mutate func(*ap.CreateInvoiceRequest)
		want   error
	}{
		{
			name: "tier direct tanpa unit",
			mutate: func(r *ap.CreateInvoiceRequest) {
				r.Lines[0].CostTier = domain.CostTierDirect
				r.Lines[0].UnitID = nil
			},
			want: cost.ErrUnitRequiredForDirect,
		},
		{
			name: "item RAB milik proyek lain",
			mutate: func(r *ap.CreateInvoiceRequest) {
				missing := uint64(999_999_999)
				r.Lines[0].BudgetItemID = &missing
			},
			want: cost.ErrBudgetItemNotFound,
		},
		{
			name: "nominal nol",
			mutate: func(r *ap.CreateInvoiceRequest) {
				r.Lines[0].Amount = domain.Zero
			},
			want: cost.ErrCostAmountZeroOrNeg,
		},
		{
			name: "kategori tidak cocok dengan tier",
			mutate: func(r *ap.CreateInvoiceRequest) {
				r.Lines[0].CostTier = domain.CostTierOverhead
			},
			want: cost.ErrTierCategoryMismatch,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := f.tagihanProyek(t, "INV/W11/BAD", "10000000")
			tc.mutate(&req)

			if _, err := f.svc.PreviewInvoice(ctx, w11Tenant, req); !errors.Is(err, tc.want) {
				t.Fatalf("pratinjau: error = %v, mau %v", err, tc.want)
			}
			if _, err := f.svc.RecordInvoice(ctx, w11Tenant, req); !errors.Is(err, tc.want) {
				t.Fatalf("pencatatan: error = %v, mau %v", err, tc.want)
			}
			// Fail-closed: tidak ada tagihan, tidak ada jurnal, tidak ada biaya.
			if n := f.countInvoices(t, w11Tenant); n != 0 {
				t.Fatalf("%d tagihan tertulis padahal ditolak", n)
			}
			var entries int64
			if err := f.db.Raw("SELECT COUNT(*) FROM cost_entries WHERE tenant_id = ?", w11Tenant).Scan(&entries).Error; err != nil {
				t.Fatalf("hitung cost_entries: %v", err)
			}
			if entries != 0 {
				t.Fatalf("%d baris biaya tertulis padahal ditolak", entries)
			}
		})
	}
}

// ── 9. Retensi & uang muka ditutup di pintu masuk ───────────────────────────

func TestAPRetensiDanUangMukaBelumDibuka(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	withRet := f.tagihanProyek(t, "INV/W11/RET", "10000000")
	withRet.RetentionAmount = w11Money(t, "500000")
	if _, err := f.svc.RecordInvoice(ctx, w11Tenant, withRet); !errors.Is(err, ap.ErrRetentionNotEnabled) {
		t.Fatalf("error = %v, mau %v", err, ap.ErrRetentionNotEnabled)
	}

	withAdv := f.tagihanProyek(t, "INV/W11/ADV", "10000000")
	withAdv.AdvanceApplied = w11Money(t, "500000")
	if _, err := f.svc.RecordInvoice(ctx, w11Tenant, withAdv); !errors.Is(err, ap.ErrAdvanceNotEnabled) {
		t.Fatalf("error = %v, mau %v", err, ap.ErrAdvanceNotEnabled)
	}

	// Tidak ada saldo yang mengendap di akun yang belum punya jalur penyelesaian.
	for _, code := range []string{"2-1100", "1-5300"} {
		if s := f.saldoAkun(t, code); !s.IsZero() {
			t.Fatalf("saldo %s = %s, mau nol", code, s)
		}
	}
}

// ── 10. Pembaca cost_entries yang sudah ada tetap melihat baris AP ──────────

func TestAPCostEntriesTerbacaPembacaLama(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	inv, err := f.svc.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/READ", "40000000"))
	if err != nil {
		t.Fatalf("catat tagihan: %v", err)
	}
	if _, err := f.svc.PostInvoice(ctx, w11Tenant, inv.ID); err != nil {
		t.Fatalf("posting: %v", err)
	}

	repo := cost.NewGORMRepository(f.db)
	entries, err := repo.ListCostEntriesByProject(ctx, w11Tenant, f.projectID)
	if err != nil {
		t.Fatalf("baca cost entries: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("pembaca lama melihat %d baris, mau 1", len(entries))
	}
	e := entries[0]
	if e.APInvoiceID == nil || *e.APInvoiceID != inv.ID {
		t.Fatalf("baris biaya tidak tertaut ke tagihan: %v", e.APInvoiceID)
	}
	if e.VendorID == nil || *e.VendorID != f.vendorPKP {
		t.Fatalf("baris biaya tidak tertaut ke vendor: %v", e.VendorID)
	}
	if e.Vendor == "" {
		t.Fatal("nama vendor kosong pada baris biaya")
	}

	// Akumulasi biaya proyek — jalur yang dipakai HPP — ikut melihatnya.
	acc, err := repo.AccumulatedProjectWide(ctx, w11Tenant, f.projectID)
	if err != nil {
		t.Fatalf("akumulasi biaya proyek: %v", err)
	}
	if got, want := acc.Total().String(), "40000000"; got != want {
		t.Fatalf("akumulasi biaya proyek = %s, mau %s", got, want)
	}
}

// ── 11. Master vendor ───────────────────────────────────────────────────────

func TestAPVendorMaster(t *testing.T) {
	f := w11Setup(t)
	ctx := context.Background()

	// Perubahan parsial tidak boleh menjatuhkan status PKP yang tidak disebut.
	nama := "PT Kontraktor Jaya Abadi"
	v, err := f.svc.UpdateVendor(ctx, w11Tenant, f.vendorPKP, ap.UpdateVendorRequest{Name: &nama})
	if err != nil {
		t.Fatalf("ubah vendor: %v", err)
	}
	if !v.IsPKP {
		t.Fatal("status PKP hilang padahal tidak diubah")
	}
	if v.Name != nama {
		t.Fatalf("nama = %s, mau %s", v.Name, nama)
	}

	// Nonaktif = tidak boleh menerima tagihan baru, tetapi barisnya tetap ada.
	off := false
	if _, err := f.svc.UpdateVendor(ctx, w11Tenant, f.vendorPKP, ap.UpdateVendorRequest{IsActive: &off}); err != nil {
		t.Fatalf("nonaktifkan vendor: %v", err)
	}
	if _, err := f.svc.RecordInvoice(ctx, w11Tenant, f.tagihanProyek(t, "INV/W11/OFF", "10000000")); !errors.Is(err, ap.ErrVendorInactive) {
		t.Fatalf("error = %v, mau %v", err, ap.ErrVendorInactive)
	}
	if _, err := f.svc.GetVendor(ctx, w11Tenant, f.vendorPKP); err != nil {
		t.Fatalf("vendor nonaktif harus tetap terbaca: %v", err)
	}
}
