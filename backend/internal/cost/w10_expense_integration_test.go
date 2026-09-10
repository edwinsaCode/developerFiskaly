//go:build integration

package cost_test

// W-10 Transaksi Pengeluaran — pembuktian terhadap MySQL nyata.
//
// Satu pintu masuk pengeluaran (operasional dan proyek) di atas mesin biaya yang
// sudah ada. Yang dibuktikan di sini adalah hal-hal yang tidak bisa dibuktikan
// unit test: akun yang benar-benar terdebit di buku besar, dokumen BKK yang
// benar-benar terbit, batas antara tag proyek dan realisasi RAB, dan bahwa
// kegagalan di tengah jalan tidak meninggalkan setengah transaksi.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (>= 000072).

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/budget"
	"esaproperti/internal/cost"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
)

const (
	w10Tenant      uint64 = 9_900_072
	w10OtherTenant uint64 = 9_900_073
)

var errW10Injected = errors.New("kegagalan disuntik W-10")

// Akun yang sengaja dinonaktifkan — umpan untuk membuktikan penjaga T-5.
var w10InactiveAccountCodes = []string{"1-1301"}

// ── Fixture ─────────────────────────────────────────────────────────────────

type w10Fixture struct {
	db  *gorm.DB
	svc *cost.Service

	projectA, projectB uint64
	unitA, unitB       uint64
	planA, itemHard    uint64

	etGaji     uint64 // 5-4100 — akun beban murni, boleh di-tag proyek
	etATK      uint64 // 5-4000 — akun taksonomi, TIDAK boleh di-tag proyek (INV-EXP-2)
	etNonaktif uint64
	etTenantB  uint64
}

func w10Connect(t *testing.T) *gorm.DB {
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

func w10Cleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	tables := []string{
		"cost_entries", "documents", "document_sequences", "journal_lines", "journal_entries",
		"accounts", "expense_types", "master_data_changes", "budget_items", "budget_plans",
		"units", "projects", "accounting_periods",
	}
	for _, tn := range tables {
		for _, tid := range []uint64{w10Tenant, w10OtherTenant} {
			if err := db.Exec("DELETE FROM "+tn+" WHERE tenant_id = ?", tid).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tn, err)
			}
		}
	}
}

// w10Accounts menanam COA minimal. Termasuk akun yang SENGAJA salah
// (4-1000 pendapatan, bank nonaktif) — tanpa umpan yang salah, penjaga tidak
// pernah terbukti benar-benar menjaga.
func w10Accounts(t *testing.T, db *gorm.DB, tenantID uint64) {
	t.Helper()
	accs := []*ledger.Account{
		{Code: "5-4100", Name: "Beban Gaji", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{Code: "5-4000", Name: "Beban Umum & Administrasi", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{Code: "5-3000", Name: "Beban Pemasaran", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{Code: "5-4200", Name: "Beban Sewa Kantor", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{Code: "1-3100", Name: "Persediaan Biaya Konstruksi", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{Code: "1-3000", Name: "Persediaan Tanah", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{Code: "1-1100", Name: "Kas", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryCash},
		{Code: "1-1300", Name: "Bank BCA", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
		{Code: "1-1301", Name: "Bank Lama (ditutup)", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, Category: ledger.CategoryBank},
		{Code: "4-1000", Name: "Pendapatan Penjualan", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{Code: "2-1000", Name: "Hutang Usaha", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
	}
	for _, a := range accs {
		a.TenantID = tenantID
		if err := db.Create(a).Error; err != nil {
			t.Fatalf("seed akun %s: %v", a.Code, err)
		}
	}
	// is_active bertag `default:true`: GORM membuang nilai false saat INSERT dan
	// justru menulis balik true ke struct-nya. Nonaktifkan lewat UPDATE eksplisit
	// berdasarkan kode — jangan mengandalkan nilai field pasca-Create.
	if err := db.Model(&ledger.Account{}).
		Where("tenant_id = ? AND code IN ?", tenantID, w10InactiveAccountCodes).
		Update("is_active", false).Error; err != nil {
		t.Fatalf("nonaktifkan akun: %v", err)
	}
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, tenantID); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

func w10Service(t *testing.T, db *gorm.DB) *cost.Service {
	t.Helper()
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).
		WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	repo := cost.NewGORMRepository(db)
	svc := cost.NewService(repo, cost.NewLedgerJournalAdapter(posting), repo, repo, nil)
	svc.SetTxRunner(cost.NewGORMTxRunner(db, posting))
	svc.SetExpenseTypeResolver(repo)
	svc.SetPaymentAccountValidator(repo)
	svc.SetUnitProjectResolver(repo)
	svc.SetExpenseReader(repo)
	svc.SetUnitPolicyResolver(project.NewGORMRepository(db))
	return svc
}

func w10Setup(t *testing.T) *w10Fixture {
	t.Helper()
	db := w10Connect(t)
	w10Cleanup(t, db)
	t.Cleanup(func() { w10Cleanup(t, db) })

	w10Accounts(t, db, w10Tenant)
	w10Accounts(t, db, w10OtherTenant)

	ctx := context.Background()
	// Katalog produk: unit properti ikut HPP (syarat biaya direct).
	for _, tid := range []uint64{w10Tenant, w10OtherTenant} {
		if err := project.SeedDefaultProductTypes(ctx, db, tid); err != nil {
			t.Fatalf("seed product types: %v", err)
		}
		if err := cost.SeedDefaultExpenseTypes(ctx, db, tid); err != nil {
			t.Fatalf("seed expense types: %v", err)
		}
	}

	f := &w10Fixture{db: db, svc: w10Service(t, db)}

	pa := &project.Project{TenantID: w10Tenant, Name: "Proyek A W10", Status: project.ProjectStatusActive}
	pb := &project.Project{TenantID: w10Tenant, Name: "Proyek B W10", Status: project.ProjectStatusActive}
	for _, p := range []*project.Project{pa, pb} {
		if err := db.Create(p).Error; err != nil {
			t.Fatalf("seed project: %v", err)
		}
	}
	f.projectA, f.projectB = pa.ID, pb.ID

	ua := &project.Unit{TenantID: w10Tenant, ProjectID: pa.ID, Code: "W10-A01", UnitType: "rumah", Status: project.UnitStatusAvailable}
	ub := &project.Unit{TenantID: w10Tenant, ProjectID: pb.ID, Code: "W10-B01", UnitType: "rumah", Status: project.UnitStatusAvailable}
	for _, u := range []*project.Unit{ua, ub} {
		if err := db.Create(u).Error; err != nil {
			t.Fatalf("seed unit: %v", err)
		}
	}
	f.unitA, f.unitB = ua.ID, ub.ID

	budgeted, _ := domain.NewMoney("100000000")
	plan := &budget.BudgetPlan{TenantID: w10Tenant, ProjectID: pa.ID, Version: 1, Label: "RAB W10", Status: budget.BudgetPlanStatusActive}
	if err := db.Create(plan).Error; err != nil {
		t.Fatalf("seed budget plan: %v", err)
	}
	item := &budget.BudgetItem{TenantID: w10Tenant, BudgetPlanID: plan.ID, Category: budget.BudgetCategoryConstruction, Subcategory: "Struktur", BudgetedAmount: budgeted}
	if err := db.Create(item).Error; err != nil {
		t.Fatalf("seed budget item: %v", err)
	}
	f.planA, f.itemHard = plan.ID, item.ID

	f.etGaji = w10TypeID(t, db, w10Tenant, "gaji")
	f.etATK = w10TypeID(t, db, w10Tenant, "atk")
	f.etTenantB = w10TypeID(t, db, w10OtherTenant, "gaji")

	nonaktif := &cost.ExpenseType{TenantID: w10Tenant, Code: "arsip", Name: "Biaya Arsip (nonaktif)", ExpenseAccountCode: "5-4200"}
	if err := db.Create(nonaktif).Error; err != nil {
		t.Fatalf("seed expense type nonaktif: %v", err)
	}
	// Sama seperti akun: `default:true` membuat INSERT mengabaikan false.
	if err := db.Model(&cost.ExpenseType{}).Where("id = ?", nonaktif.ID).
		Update("is_active", false).Error; err != nil {
		t.Fatalf("nonaktifkan expense type: %v", err)
	}
	f.etNonaktif = nonaktif.ID

	return f
}

func w10TypeID(t *testing.T, db *gorm.DB, tenantID uint64, code string) uint64 {
	t.Helper()
	var et cost.ExpenseType
	if err := db.Where("tenant_id = ? AND code = ?", tenantID, code).First(&et).Error; err != nil {
		t.Fatalf("cari expense type %s: %v", code, err)
	}
	return et.ID
}

// ── Helper request ──────────────────────────────────────────────────────────

func w10Money(t *testing.T, s string) domain.Money {
	t.Helper()
	m, err := domain.NewMoney(s)
	if err != nil {
		t.Fatalf("nominal %q: %v", s, err)
	}
	return m
}

// operasional membangun request pengeluaran kantor persis seperti yang
// dihasilkan handler untuk scope "operasional".
func (f *w10Fixture) operasional(t *testing.T, expenseTypeID uint64, amount string) cost.CreateCostEntryRequest {
	t.Helper()
	id := expenseTypeID
	return cost.CreateCostEntryRequest{
		ExpenseTypeID:   &id,
		Category:        domain.CostCategoryOther,
		CostTier:        domain.CostTierOverhead,
		Amount:          w10Money(t, amount),
		PaymentMethod:   cost.PaymentMethodBank,
		BankAccountCode: "1-1300",
		Date:            time.Now(),
		Vendor:          "PT Contoh W10",
		Description:     "Pengeluaran kantor W10",
	}
}

// proyek membangun request biaya proyek (taksonomi CostCategory).
func (f *w10Fixture) proyek(t *testing.T, projectID uint64, amount string) cost.CreateCostEntryRequest {
	t.Helper()
	return cost.CreateCostEntryRequest{
		ProjectID: projectID,
		Category:  domain.CostCategoryHard,
		CostTier:  domain.CostTierShared,
		// UAT 2026-09-07: wajib diisi untuk tier=shared. Sarana & Prasarana
		// dipilih di sini karena test-test W-10 ini menguji mekanika
		// pencatatan/dokumen, bukan alokasi Subsidi/Komersial — subkategori
		// yang tidak restricted-by-tax-category menjaga fixture tetap netral.
		HardSubcategory: domain.ConstructionSaranaPrasarana,
		Amount:          w10Money(t, amount),
		PaymentMethod:   cost.PaymentMethodBank,
		BankAccountCode: "1-1300",
		Date:            time.Now(),
		Vendor:          "CV Konstruksi W10",
		Description:     "Material proyek W10",
	}
}

func (f *w10Fixture) lines(t *testing.T, journalID uint64) []ledger.JournalLine {
	t.Helper()
	var lines []ledger.JournalLine
	if err := f.db.Preload("Account").
		Where("tenant_id = ? AND journal_entry_id = ?", w10Tenant, journalID).
		Order("id").Find(&lines).Error; err != nil {
		t.Fatalf("baca journal lines: %v", err)
	}
	return lines
}

// assertDebitCredit membuktikan jurnal 2 baris: debit di debitCode, kredit di
// creditCode, keduanya sebesar amount.
func (f *w10Fixture) assertDebitCredit(t *testing.T, journalID uint64, debitCode, creditCode, amount string) {
	t.Helper()
	lines := f.lines(t, journalID)
	if len(lines) != 2 {
		t.Fatalf("jurnal harus 2 baris, ada %d", len(lines))
	}
	var gotDebit, gotCredit string
	var debitAcc, creditAcc string
	for _, l := range lines {
		if !l.Debit.IsZero() {
			gotDebit, debitAcc = l.Debit.String(), l.Account.Code
		}
		if !l.Credit.IsZero() {
			gotCredit, creditAcc = l.Credit.String(), l.Account.Code
		}
	}
	if debitAcc != debitCode {
		t.Errorf("akun debit = %s, harusnya %s", debitAcc, debitCode)
	}
	if creditAcc != creditCode {
		t.Errorf("akun kredit = %s, harusnya %s", creditAcc, creditCode)
	}
	want := w10Money(t, amount).String()
	if gotDebit != want || gotCredit != want {
		t.Errorf("nominal debit=%s kredit=%s, harusnya %s", gotDebit, gotCredit, want)
	}
	if gotDebit != gotCredit {
		t.Errorf("jurnal tidak balanced: debit %s vs kredit %s", gotDebit, gotCredit)
	}
}

// ── 1. Pengeluaran kantor → GL benar ────────────────────────────────────────

func TestW10_1_PengeluaranKantor_JurnalBenar(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etGaji, "12500000"))
	if err != nil {
		t.Fatalf("CreateAndPostCostEntry: %v", err)
	}
	// Dr Beban Gaji (dari MASTER, bukan dari enum kategori) / Cr Bank.
	f.assertDebitCredit(t, e.JournalEntryID, "5-4100", "1-1300", "12500000")

	var je ledger.JournalEntry
	if err := f.db.First(&je, "id = ?", e.JournalEntryID).Error; err != nil {
		t.Fatalf("baca jurnal: %v", err)
	}
	if je.PostedAt == nil {
		t.Error("jurnal harus langsung terposting (create+post atomik)")
	}
	if e.ProjectID != nil {
		t.Error("pengeluaran kantor murni tidak boleh punya project_id")
	}
}

// ── 2. Pengeluaran kantor → BKK terbit ──────────────────────────────────────

func TestW10_2_PengeluaranKantor_MenerbitkanBKK(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etGaji, "3000000"))
	if err != nil {
		t.Fatalf("CreateAndPostCostEntry: %v", err)
	}

	var je ledger.JournalEntry
	if err := f.db.First(&je, "id = ?", e.JournalEntryID).Error; err != nil {
		t.Fatalf("baca jurnal: %v", err)
	}
	if je.DocumentID == nil {
		t.Fatal("INV-DOC-1: jurnal kas keluar wajib tertaut dokumen")
	}
	var doc document.Document
	if err := f.db.First(&doc, "id = ? AND tenant_id = ?", *je.DocumentID, w10Tenant).Error; err != nil {
		t.Fatalf("baca dokumen: %v", err)
	}
	if doc.DocumentTypeCode != string(ledger.DocCashOut) {
		t.Errorf("jenis dokumen = %s, harusnya %s (BKK)", doc.DocumentTypeCode, ledger.DocCashOut)
	}
	if doc.Number == "" {
		t.Error("dokumen wajib bernomor")
	}

	// Bukti itu juga harus sampai ke layar, bukan hanya ada di database.
	item, err := f.svc.GetExpense(ctx, w10Tenant, e.ID)
	if err != nil {
		t.Fatalf("GetExpense: %v", err)
	}
	if item.DocumentNumber != doc.Number {
		t.Errorf("nomor dokumen di daftar = %q, harusnya %q", item.DocumentNumber, doc.Number)
	}
	if item.Status != cost.ExpenseStatusPosted {
		t.Errorf("status = %q, harusnya posted", item.Status)
	}
	if item.ExpenseTypeName == "" {
		t.Error("nama jenis pengeluaran harus ikut di daftar")
	}
}

// ── 3 & 4. Biaya proyek → GL benar + terlihat sebagai biaya proyek ──────────

func TestW10_3_4_BiayaProyek_JurnalDanBiayaProyek(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.proyek(t, f.projectA, "45000000"))
	if err != nil {
		t.Fatalf("CreateAndPostCostEntry: %v", err)
	}
	// Kapitalisasi, BUKAN beban: Dr Persediaan.
	f.assertDebitCredit(t, e.JournalEntryID, "1-3100", "1-1300", "45000000")

	// §16: transaksi W-10 yang ber-scope proyek harus muncul di Proyek → Biaya.
	entries, err := f.svc.ListByProject(ctx, w10Tenant, f.projectA)
	if err != nil {
		t.Fatalf("ListByProject: %v", err)
	}
	if len(entries) != 1 || entries[0].ID != e.ID {
		t.Fatalf("biaya W-10 harus muncul di daftar biaya proyek; dapat %d baris", len(entries))
	}

	// Dan harus ikut terakumulasi sebagai biaya proyek (sumber: jurnal terposting).
	acc, err := f.svc.AccumulatedByProject(ctx, w10Tenant, f.projectA)
	if err != nil {
		t.Fatalf("AccumulatedByProject: %v", err)
	}
	if acc.Hard.String() != w10Money(t, "45000000").String() {
		t.Errorf("akumulasi hard = %s, harusnya 45000000", acc.Hard.String())
	}
}

// ── 5 & 6. RAB: tautan item = realisasi; tag proyek saja = BUKAN (BD-2) ─────

func TestW10_5_6_RealisasiRAB_HanyaDariTautanItem(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	// (a) dengan budget_item_id → realisasi item.
	withItem := f.proyek(t, f.projectA, "20000000")
	withItem.BudgetItemID = &f.itemHard
	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, withItem); err != nil {
		t.Fatalf("biaya ber-RAB: %v", err)
	}
	// (b) tanpa budget_item_id, hanya ber-tag proyek → TIDAK masuk realisasi item.
	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.proyek(t, f.projectA, "7000000")); err != nil {
		t.Fatalf("biaya tanpa RAB: %v", err)
	}

	prov := budget.NewGORMRealisasiProvider(f.db)
	perItem, err := prov.GetRealisasiPerItem(ctx, w10Tenant, f.planA)
	if err != nil {
		t.Fatalf("GetRealisasiPerItem: %v", err)
	}
	got := perItem[f.itemHard]
	if got.String() != w10Money(t, "20000000").String() {
		t.Errorf("realisasi item = %s, harusnya 20000000 (yang 7 juta tanpa tautan RAB tidak boleh ikut)", got.String())
	}
}

// INV-EXP-2: pengeluaran operasional yang akunnya termasuk taksonomi biaya
// proyek tidak boleh di-tag proyek — kalau lolos, tag itu akan terbaca sebagai
// realisasi RAB per kategori dan BD-2 bocor lewat pintu belakang.
func TestW10_6b_OperasionalAkunTaksonomi_TidakBolehTagProyek(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	req := f.operasional(t, f.etATK, "500000") // atk → 5-4000 (akun taksonomi)
	req.ProjectID = f.projectA
	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); !errors.Is(err, cost.ErrExpenseTypeTaxonomyAccount) {
		t.Fatalf("harus ErrExpenseTypeTaxonomyAccount, dapat %v", err)
	}

	// Tanpa tag proyek, jenis yang sama tetap boleh dipakai.
	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etATK, "500000")); err != nil {
		t.Fatalf("tanpa tag proyek harus boleh: %v", err)
	}
}

// Jenis pengeluaran ber-akun beban murni BOLEH di-tag proyek (cost center),
// dan tag itu tetap TIDAK menjadikannya realisasi RAB per item.
func TestW10_6c_OperasionalTagProyek_BukanRealisasiRAB(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	req := f.operasional(t, f.etGaji, "9000000")
	req.ProjectID = f.projectA
	e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req)
	if err != nil {
		t.Fatalf("operasional ber-tag proyek: %v", err)
	}
	if e.ProjectID == nil || *e.ProjectID != f.projectA {
		t.Fatal("tag proyek harus tersimpan")
	}

	prov := budget.NewGORMRealisasiProvider(f.db)
	perItem, err := prov.GetRealisasiPerItem(ctx, w10Tenant, f.planA)
	if err != nil {
		t.Fatalf("GetRealisasiPerItem: %v", err)
	}
	if len(perItem) != 0 {
		t.Errorf("tag proyek tidak boleh melahirkan realisasi RAB; dapat %v", perItem)
	}
	byCC, err := prov.GetRealisasiByProject(ctx, w10Tenant, f.projectA, nil)
	if err != nil {
		t.Fatalf("GetRealisasiByProject: %v", err)
	}
	if v, ok := byCC[domain.CostCategoryOther]; ok && !v.IsZero() {
		t.Errorf("beban gaji (5-4100) tidak boleh terbaca sebagai realisasi kategori RAB; dapat %s", v.String())
	}
}

// ── 7 & 8. Unit tagging ─────────────────────────────────────────────────────

func TestW10_7_UnitTaggingValid(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	req := f.proyek(t, f.projectA, "15000000")
	req.CostTier = domain.CostTierDirect
	req.UnitID = &f.unitA
	e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req)
	if err != nil {
		t.Fatalf("biaya direct ke unit sendiri harus lolos: %v", err)
	}
	acc, err := f.svc.AccumulatedByUnit(ctx, w10Tenant, f.projectA, f.unitA)
	if err != nil {
		t.Fatalf("AccumulatedByUnit: %v", err)
	}
	if acc.Hard.String() != w10Money(t, "15000000").String() {
		t.Errorf("biaya unit = %s, harusnya 15000000", acc.Hard.String())
	}
	if e.UnitID == nil || *e.UnitID != f.unitA {
		t.Error("unit tag harus tersimpan di cost entry")
	}
}

func TestW10_8_UnitDariProyekLain_Ditolak(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	req := f.proyek(t, f.projectA, "15000000")
	req.CostTier = domain.CostTierDirect
	req.UnitID = &f.unitB // milik proyek B
	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); !errors.Is(err, cost.ErrUnitNotInProject) {
		t.Fatalf("harus ErrUnitNotInProject, dapat %v", err)
	}
	if n := w10Count(t, f.db, "journal_entries", w10Tenant); n != 0 {
		t.Errorf("penolakan tidak boleh meninggalkan jurnal; ada %d", n)
	}
}

// ── 9 & 10. Master jenis pengeluaran ────────────────────────────────────────

func TestW10_9_JenisNonaktif_Ditolak(t *testing.T) {
	f := w10Setup(t)
	if _, err := f.svc.CreateAndPostCostEntry(context.Background(), w10Tenant,
		f.operasional(t, f.etNonaktif, "1000000")); !errors.Is(err, cost.ErrExpenseTypeInactive) {
		t.Fatalf("harus ErrExpenseTypeInactive, dapat %v", err)
	}
}

func TestW10_10_JenisTenantLain_Ditolak(t *testing.T) {
	f := w10Setup(t)
	// Jenis milik tenant B dipakai tenant A: harus tak terlihat sama sekali.
	if _, err := f.svc.CreateAndPostCostEntry(context.Background(), w10Tenant,
		f.operasional(t, f.etTenantB, "1000000")); !errors.Is(err, cost.ErrExpenseTypeUnknown) {
		t.Fatalf("harus ErrExpenseTypeUnknown (isolasi tenant), dapat %v", err)
	}
	// Sebaliknya juga: tenant B tidak melihat jenis milik tenant A.
	req := f.operasional(t, f.etGaji, "1000000")
	if _, err := f.svc.CreateAndPostCostEntry(context.Background(), w10OtherTenant, req); !errors.Is(err, cost.ErrExpenseTypeUnknown) {
		t.Fatalf("arah sebaliknya harus ErrExpenseTypeUnknown, dapat %v", err)
	}
}

func TestW10_10b_JenisUnknown_TidakJatuhKeAkunDefault(t *testing.T) {
	f := w10Setup(t)
	if _, err := f.svc.CreateAndPostCostEntry(context.Background(), w10Tenant,
		f.operasional(t, 999_999_999, "1000000")); !errors.Is(err, cost.ErrExpenseTypeUnknown) {
		t.Fatalf("fail-closed: harus ErrExpenseTypeUnknown, dapat %v", err)
	}
	if n := w10Count(t, f.db, "cost_entries", w10Tenant); n != 0 {
		t.Errorf("tidak boleh ada cost entry; ada %d", n)
	}
}

// ── 11 & 12. T-5 akun pembayaran ────────────────────────────────────────────

func TestW10_11_AkunPembayaranBukanKasBank_Ditolak(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	for _, code := range []string{"4-1000", "5-4100", "2-1000"} {
		req := f.operasional(t, f.etGaji, "1000000")
		req.BankAccountCode = code
		if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); !errors.Is(err, cost.ErrNotCashBankAccount) {
			t.Errorf("akun %s: harus ErrNotCashBankAccount, dapat %v", code, err)
		}
	}
	// Akun kas dan bank yang sah tetap diterima.
	for _, code := range []string{"1-1100", "1-1300"} {
		req := f.operasional(t, f.etGaji, "1000000")
		req.BankAccountCode = code
		if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); err != nil {
			t.Errorf("akun %s harus diterima: %v", code, err)
		}
	}
}

func TestW10_12_AkunBankNonaktif_Ditolak(t *testing.T) {
	f := w10Setup(t)
	req := f.operasional(t, f.etGaji, "1000000")
	req.BankAccountCode = "1-1301" // bank nonaktif
	if _, err := f.svc.CreateAndPostCostEntry(context.Background(), w10Tenant, req); !errors.Is(err, cost.ErrNotCashBankAccount) {
		t.Fatalf("harus ErrNotCashBankAccount, dapat %v", err)
	}
}

// Mapping akun beban juga divalidasi saat master DIBUAT — bukan saat kas keluar.
func TestW10_12b_MasterMenolakAkunNonBeban(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()
	svc := cost.NewExpenseTypeService(f.db)

	for _, code := range []string{"1-1300", "4-1000", "2-1000", "9-9999"} {
		_, err := svc.Create(ctx, w10Tenant, cost.ExpenseTypeInput{
			Code: "uji-" + code, Name: "Uji " + code, ExpenseAccountCode: code,
		}, nil)
		if !errors.Is(err, cost.ErrExpenseAccountInvalid) {
			t.Errorf("akun %s: harus ErrExpenseAccountInvalid, dapat %v", code, err)
		}
	}

	et, err := svc.Create(ctx, w10Tenant, cost.ExpenseTypeInput{
		Code: "Konsultan", Name: "Jasa Konsultan", ExpenseAccountCode: "5-4200",
	}, nil)
	if err != nil {
		t.Fatalf("akun beban sah harus diterima: %v", err)
	}
	if et.Code != "konsultan" {
		t.Errorf("kode harus dinormalisasi lowercase, dapat %q", et.Code)
	}

	// Perubahan mapping tercatat sebagai audit append-only.
	newAcc := "5-4000"
	if _, err := svc.Update(ctx, w10Tenant, et.ID, cost.ExpenseTypeUpdate{ExpenseAccountCode: &newAcc}, nil); err != nil {
		t.Fatalf("Update: %v", err)
	}
	changes, err := svc.ListMasterChanges(ctx, w10Tenant, 10)
	if err != nil {
		t.Fatalf("ListMasterChanges: %v", err)
	}
	if len(changes) < 2 {
		t.Errorf("perubahan mapping wajib teraudit; hanya %d catatan", len(changes))
	}
}

// ── 13. Jurnal tidak seimbang mustahil ──────────────────────────────────────

func TestW10_13_JurnalTidakSeimbang_Mustahil(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	ledgerRepo := ledger.NewGORMRepository(f.db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)

	var beban, bank ledger.Account
	f.db.First(&beban, "tenant_id = ? AND code = ?", w10Tenant, "5-4100")
	f.db.First(&bank, "tenant_id = ? AND code = ?", w10Tenant, "1-1300")

	_, err := posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: w10Tenant, Date: time.Now(), Description: "Uji tidak seimbang W10",
		Lines: []ledger.LineInput{
			{AccountID: beban.ID, Debit: w10Money(t, "1000000"), Credit: domain.Zero},
			{AccountID: bank.ID, Debit: domain.Zero, Credit: w10Money(t, "900000")},
		},
	})
	if err == nil {
		t.Fatal("jurnal tidak seimbang harus ditolak (Invariant #1)")
	}
	if n := w10Count(t, f.db, "journal_entries", w10Tenant); n != 0 {
		t.Errorf("penolakan tidak boleh menyisakan jurnal; ada %d", n)
	}
}

// ── 14. Gagal terbit dokumen → seluruh transaksi batal ──────────────────────

func TestW10_14_GagalTerbitDokumen_RollbackPenuh(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	// Cabut master jenis dokumen: penerbitan BKK fail-closed (INV-DOC-1).
	if err := f.db.Exec("DELETE FROM document_types WHERE tenant_id = ?", w10Tenant).Error; err != nil {
		t.Fatalf("hapus jenis dokumen: %v", err)
	}

	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etGaji, "1000000")); err == nil {
		t.Fatal("tanpa jenis dokumen, posting kas keluar harus gagal")
	}
	if n := w10Count(t, f.db, "cost_entries", w10Tenant); n != 0 {
		t.Errorf("cost entry harus ikut dibatalkan; ada %d", n)
	}
	if n := w10Count(t, f.db, "journal_entries", w10Tenant); n != 0 {
		t.Errorf("jurnal harus ikut dibatalkan; ada %d", n)
	}
	if n := w10Count(t, f.db, "documents", w10Tenant); n != 0 {
		t.Errorf("tidak boleh ada dokumen; ada %d", n)
	}
}

// ── 15. Gagal menyimpan cost entry → jurnal ikut batal ──────────────────────

type w10FailStore struct{ cost.CostStore }

func (w10FailStore) CreateCostEntry(_ context.Context, _ *cost.CostEntry) error { return errW10Injected }

type w10FailRunner struct{ inner cost.TxRunner }

func (w w10FailRunner) InTx(ctx context.Context, fn func(cost.JournalWriter, cost.CostStore) error) error {
	return w.inner.InTx(ctx, func(jw cost.JournalWriter, st cost.CostStore) error {
		return fn(jw, w10FailStore{st})
	})
}

func TestW10_15_GagalSimpanEntry_RollbackJurnal(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	ledgerRepo := ledger.NewGORMRepository(f.db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	repo := cost.NewGORMRepository(f.db)
	svc := cost.NewService(repo, cost.NewLedgerJournalAdapter(posting), repo, repo, nil)
	svc.SetExpenseTypeResolver(repo)
	svc.SetPaymentAccountValidator(repo)
	svc.SetTxRunner(w10FailRunner{inner: cost.NewGORMTxRunner(f.db, posting)})

	if _, err := svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etGaji, "1000000")); !errors.Is(err, errW10Injected) {
		t.Fatalf("harus kegagalan yang disuntik, dapat %v", err)
	}
	for _, tbl := range []string{"journal_entries", "journal_lines", "cost_entries", "documents"} {
		if n := w10Count(t, f.db, tbl, w10Tenant); n != 0 {
			t.Errorf("%s harus kosong setelah rollback; ada %d", tbl, n)
		}
	}
}

// ── 16. Periode tertutup ────────────────────────────────────────────────────

func TestW10_16_PeriodeTertutup_Ditolak(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	closedDate := time.Date(2024, 3, 15, 0, 0, 0, 0, time.Local)
	if _, err := ledger.NewGORMRepository(f.db).UpsertPeriodClose(ctx, w10Tenant, 2024, 3, 1); err != nil {
		t.Fatalf("tutup periode: %v", err)
	}

	req := f.operasional(t, f.etGaji, "1000000")
	req.Date = closedDate
	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); !errors.Is(err, ledger.ErrPeriodClosed) {
		t.Fatalf("harus ErrPeriodClosed, dapat %v", err)
	}
	if n := w10Count(t, f.db, "cost_entries", w10Tenant); n != 0 {
		t.Errorf("tidak boleh ada cost entry di periode tertutup; ada %d", n)
	}
}

// ── 17. Biaya terposting bersifat immutable ─────────────────────────────────

func TestW10_17_BiayaTerposting_Immutable(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etGaji, "2000000"))
	if err != nil {
		t.Fatalf("CreateAndPostCostEntry: %v", err)
	}
	// Memposting ulang tidak boleh menghasilkan efek akuntansi kedua.
	if err := f.svc.PostCostEntry(ctx, w10Tenant, e.ID); err == nil {
		t.Error("posting ulang jurnal yang sudah diposting harus ditolak")
	}
	if n := w10Count(t, f.db, "journal_lines", w10Tenant); n != 2 {
		t.Errorf("harus tetap 2 baris jurnal, ada %d", n)
	}
	if n := w10Count(t, f.db, "documents", w10Tenant); n != 1 {
		t.Errorf("harus tetap 1 dokumen (tidak ada BKK kedua), ada %d", n)
	}
}

// ── 18. Pembalikan tidak melahirkan status/saldo yang drift ─────────────────

func TestW10_18_Pembalikan_StatusTurunanIkut(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etGaji, "4000000"))
	if err != nil {
		t.Fatalf("CreateAndPostCostEntry: %v", err)
	}

	ledgerRepo := ledger.NewGORMRepository(f.db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	if _, err := posting.Reverse(ctx, w10Tenant, e.JournalEntryID, time.Now()); err != nil {
		t.Fatalf("Reverse: %v", err)
	}

	// §8: status DITURUNKAN dari jurnal. Tidak ada kolom yang perlu disinkronkan,
	// jadi tidak ada yang bisa drift.
	item, err := f.svc.GetExpense(ctx, w10Tenant, e.ID)
	if err != nil {
		t.Fatalf("GetExpense: %v", err)
	}
	if item.Status != cost.ExpenseStatusReversed {
		t.Errorf("status = %q, harusnya reversed", item.Status)
	}

	// Dan angkanya benar-benar netral di buku besar.
	byCode, err := ledger.NewQueryService(f.db).ActualCostByCode(ctx, w10Tenant, []string{"5-4100"}, ledger.ActualCostScope{})
	if err != nil {
		t.Fatalf("ActualCostByCode: %v", err)
	}
	if v, ok := byCode["5-4100"]; ok && !v.IsZero() {
		t.Errorf("setelah dibalik, beban netto harus nol; dapat %s", v.String())
	}
}

// ── 19. Pengiriman ganda/bersamaan ──────────────────────────────────────────

// Dua submit identik yang berbarengan menghasilkan DUA transaksi terpisah —
// tidak ada idempotency key di W-10, dan itu memang perilaku yang diakui.
// Yang WAJIB dijamin: tidak ada nomor dokumen kembar dan tidak ada tulisan
// setengah jadi.
func TestW10_19_SubmitBersamaan_TidakTumpangTindih(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.operasional(t, f.etGaji, "1000000"))
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("submit #%d gagal: %v", i, err)
		}
	}

	if got := w10Count(t, f.db, "cost_entries", w10Tenant); got != n {
		t.Errorf("harus %d cost entry, ada %d", n, got)
	}
	var distinct int64
	if err := f.db.Table("documents").Where("tenant_id = ?", w10Tenant).
		Distinct("number").Count(&distinct).Error; err != nil {
		t.Fatalf("hitung nomor dokumen: %v", err)
	}
	if distinct != n {
		t.Errorf("nomor dokumen harus unik: %d nomor untuk %d transaksi", distinct, n)
	}
}

// ── 20. Satu transaksi = satu efek akuntansi ────────────────────────────────

func TestW10_20_SatuTransaksiSatuEfekAkuntansi(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, f.proyek(t, f.projectA, "33000000")); err != nil {
		t.Fatalf("CreateAndPostCostEntry: %v", err)
	}
	if n := w10Count(t, f.db, "cost_entries", w10Tenant); n != 1 {
		t.Errorf("harus 1 cost entry, ada %d", n)
	}
	if n := w10Count(t, f.db, "journal_entries", w10Tenant); n != 1 {
		t.Errorf("harus 1 jurnal, ada %d (tidak boleh transaksi → jurnal → cost entry → jurnal lagi)", n)
	}
	if n := w10Count(t, f.db, "journal_lines", w10Tenant); n != 2 {
		t.Errorf("harus 2 baris jurnal, ada %d", n)
	}
	if n := w10Count(t, f.db, "documents", w10Tenant); n != 1 {
		t.Errorf("harus 1 dokumen, ada %d", n)
	}

	// Dan biaya proyek tidak terhitung dua kali.
	acc, err := f.svc.AccumulatedByProject(ctx, w10Tenant, f.projectA)
	if err != nil {
		t.Fatalf("AccumulatedByProject: %v", err)
	}
	if acc.Hard.String() != w10Money(t, "33000000").String() {
		t.Errorf("akumulasi = %s, harusnya 33000000", acc.Hard.String())
	}
}

// ── Batas jalur operasional ─────────────────────────────────────────────────

func TestW10_BatasJalurOperasional(t *testing.T) {
	f := w10Setup(t)
	ctx := context.Background()

	t.Run("tidak boleh tertaut item RAB", func(t *testing.T) {
		req := f.operasional(t, f.etGaji, "1000000")
		req.ProjectID = f.projectA
		req.BudgetItemID = &f.itemHard
		if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); !errors.Is(err, cost.ErrExpenseTypeWithBudgetItem) {
			t.Fatalf("harus ErrExpenseTypeWithBudgetItem, dapat %v", err)
		}
	})

	t.Run("tidak boleh dipakai untuk kapitalisasi", func(t *testing.T) {
		req := f.operasional(t, f.etGaji, "1000000")
		req.ProjectID = f.projectA
		req.CostTier = domain.CostTierShared
		req.Category = domain.CostCategoryHard
		if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); !errors.Is(err, cost.ErrExpenseTypeNotForProjectCost) {
			t.Fatalf("harus ErrExpenseTypeNotForProjectCost, dapat %v", err)
		}
	})

	t.Run("tidak boleh ber-unit", func(t *testing.T) {
		req := f.operasional(t, f.etGaji, "1000000")
		req.ProjectID = f.projectA
		req.UnitID = &f.unitA
		if _, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req); !errors.Is(err, cost.ErrUnitNotAllowedForTier) {
			t.Fatalf("harus ErrUnitNotAllowedForTier, dapat %v", err)
		}
	})

	t.Run("pratinjau sama dengan jurnal yang terbit", func(t *testing.T) {
		req := f.operasional(t, f.etGaji, "1750000")
		lines, err := f.svc.PreviewCostEntry(ctx, w10Tenant, req)
		if err != nil {
			t.Fatalf("PreviewCostEntry: %v", err)
		}
		if len(lines) != 2 || lines[0].AccountCode != "5-4100" || lines[1].AccountCode != "1-1300" {
			t.Fatalf("pratinjau salah: %+v", lines)
		}
		e, err := f.svc.CreateAndPostCostEntry(ctx, w10Tenant, req)
		if err != nil {
			t.Fatalf("CreateAndPostCostEntry: %v", err)
		}
		f.assertDebitCredit(t, e.JournalEntryID, "5-4100", "1-1300", "1750000")
	})
}

func w10Count(t *testing.T, db *gorm.DB, table string, tenantID uint64) int64 {
	t.Helper()
	var n int64
	if err := db.Table(table).Where("tenant_id = ?", tenantID).Count(&n).Error; err != nil {
		t.Fatalf("hitung %s: %v", table, err)
	}
	return n
}
