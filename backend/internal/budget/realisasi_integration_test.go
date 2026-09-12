//go:build integration

package budget_test

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// P1-1 + S5: bukti Decision A (posted-only) — realisasi HANYA menghitung baris
// LEDGER yang jurnalnya sudah DIPOSTING; draft (posted_at NULL) TIDAK dihitung.
// S5 (registry #13): realisasi dibaca dari journal_lines via pembaca kanonik
// ledger.ActualCostByCode — bukan lagi Σ cost_entries.amount — sehingga test
// ini menyemai jurnal + baris jurnal produksi (COA via ledger.SeedCOA).
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (sama seperti sale integration test).

const bTenant uint64 = 9_900_002

func bConnect(t *testing.T) *gorm.DB {
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

func bCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"ap_payment_allocations", "ap_payments", "ap_invoices", "vendors",
		"budget_items", "budget_plans",
		"cost_entries", "journal_lines", "journal_entries", "accounts", "projects",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", bTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func bSeedProject(t *testing.T, db *gorm.DB, id uint64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO projects (id, tenant_id, name, status, created_at, updated_at)
		VALUES (?,?,?,?,NOW(3),NOW(3))`, id, bTenant, "IT Project", "planning").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

// bSeedJournal membuat journal_entries; posted=true → posted_at diisi.
func bSeedJournal(t *testing.T, db *gorm.DB, posted bool) uint64 {
	t.Helper()
	var postedAt interface{}
	if posted {
		postedAt = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	}
	res := db.Exec(`INSERT INTO journal_entries (tenant_id, date, description, posted_at, source, is_reversing, created_at, updated_at)
		VALUES (?,?,?,?,?,0,NOW(3),NOW(3))`,
		bTenant, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), "test", postedAt, "system")
	if res.Error != nil {
		t.Fatalf("seed journal: %v", res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

// bSeedJournalWithSource sama seperti bSeedJournal tapi dengan source eksplisit
// (dipakai untuk menyemai jurnal kapitalisasi RAB ber-source "rab_capitalization").
func bSeedJournalWithSource(t *testing.T, db *gorm.DB, posted bool, source string) uint64 {
	t.Helper()
	var postedAt interface{}
	if posted {
		postedAt = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	}
	res := db.Exec(`INSERT INTO journal_entries (tenant_id, date, description, posted_at, source, is_reversing, created_at, updated_at)
		VALUES (?,?,?,?,?,0,NOW(3),NOW(3))`,
		bTenant, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), "test", postedAt, source)
	if res.Error != nil {
		t.Fatalf("seed journal: %v", res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func bSeedCost(t *testing.T, db *gorm.DB, projectID, journalID uint64, cat string, amount int64) {
	t.Helper()
	// cost_tier NOT NULL sejak 000035: kategori beban → overhead, lainnya → shared
	// (seed tanpa unit_id = pool proyek), konsisten dengan aturan backfill.
	tier := "shared"
	if cat == "marketing" || cat == "other" {
		tier = "overhead"
	}
	if err := db.Exec(`INSERT INTO cost_entries (tenant_id, project_id, category, cost_tier, amount, payment_method, bank_account_code, date, vendor, description, journal_entry_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,NOW(3),NOW(3))`,
		bTenant, projectID, cat, tier, domain.FromInt(amount), "bank", "1-1300", "2026-07-01", "V", "", journalID).Error; err != nil {
		t.Fatalf("seed cost: %v", err)
	}
	// S5: realisasi dibaca dari LEDGER — semai baris jurnal seperti posting
	// produksi cost.Service: Dr akun kategori (project-tagged) / Cr 2-1000.
	cc := domain.CostCategory(cat)
	debitCode := cc.InventoryAccountCode()
	if cc.IsExpense() {
		debitCode = cc.ExpenseAccountCode()
	}
	for _, ln := range []struct {
		code  string
		debit int64
	}{{debitCode, amount}, {"2-1000", 0}} {
		var accID uint64
		db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = ?`, bTenant, ln.code).Scan(&accID)
		if accID == 0 {
			t.Fatalf("akun %s tidak ada (SeedCOA belum jalan?)", ln.code)
		}
		debit, credit := domain.FromInt(ln.debit), domain.Zero
		if ln.debit == 0 {
			debit, credit = domain.Zero, domain.FromInt(amount)
		}
		if err := db.Exec(`INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit, project_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,NOW(3),NOW(3))`,
			bTenant, journalID, accID, debit, credit, projectID).Error; err != nil {
			t.Fatalf("seed journal line %s: %v", ln.code, err)
		}
	}
}

func TestIntegration_Realisasi_PostedOnly(t *testing.T) {
	db := bConnect(t)
	bCleanup(t, db)
	defer bCleanup(t, db)

	const project = uint64(555)
	bSeedProject(t, db, project)
	if err := ledger.SeedCOA(context.Background(), db, bTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	postedJ := bSeedJournal(t, db, true)
	draftJ := bSeedJournal(t, db, false)
	bSeedCost(t, db, project, postedJ, "hard", 100_000_000) // POSTED → dihitung
	bSeedCost(t, db, project, draftJ, "hard", 50_000_000)   // DRAFT → TIDAK dihitung
	bSeedCost(t, db, project, postedJ, "land", 20_000_000)  // POSTED land

	prov := budget.NewGORMRealisasiProvider(db)
	got, err := prov.GetRealisasiByProject(context.Background(), bTenant, project, nil)
	if err != nil {
		t.Fatalf("GetRealisasiByProject: %v", err)
	}
	if v := got[domain.CostCategoryHard]; v.String() != "100000000" {
		t.Errorf("hard realisasi (posted-only): got %s, want 100000000 (draft 50jt dikecualikan)", v.String())
	}
	if v := got[domain.CostCategoryLand]; v.String() != "20000000" {
		t.Errorf("land realisasi: got %s, want 20000000", v.String())
	}
}

// Item 2 (2026-09-05): bukti fix "RAB vs Realisasi" untuk kategori Hard —
// kapitalisasi 100% saat approval (jurnal Dr 1-3100/Cr 2-1000, TANPA baris
// cost_entries) tidak boleh membuat realisasi langsung 100%, dan biaya aktual
// setelahnya (yang di-redirect MENJAUH dari 1-3100 oleh cost.Service.plan(),
// lihat komentar GetRealisasiByProject) harus tetap terhitung sebagai
// realisasi TANPA menyentuh/menaikkan saldo 1-3100 lagi (no double-capitalize).
// TestIntegration_Realisasi_Hard_ExcludesLegacyCapitalizationJournal
// (Item 8, UAT 2026-09-07): RULE KLIEN 2026-09-04 (kapitalisasi Construction
// penuh saat RAB approval) DICABUT klien — package ini tidak lagi menulis
// jurnal ber-source "rab_capitalization". Tapi tenant yang sempat memakai
// rule lama bisa punya jurnal historis semacam itu di data mereka; realisasi
// TETAP harus mengecualikannya (ExcludeSource defensif) supaya tidak
// terhitung ganda bersama biaya aktual yang genuinely masuk lewat cost entry.
//
// Di bawah rule baru, cost entry Hard SELALU mendebit Persediaan (1-3100)
// langsung (tidak ada redirect ke Hutang Usaha) — jadi realisasi Hard =
// murni saldo 1-3100 dari jurnal ber-tag proyek, DIKURANGI jurnal
// "rab_capitalization" historis manapun.
func TestIntegration_Realisasi_Hard_ExcludesLegacyCapitalizationJournal(t *testing.T) {
	db := bConnect(t)
	bCleanup(t, db)
	defer bCleanup(t, db)

	const project = uint64(557)
	bSeedProject(t, db, project)
	if err := ledger.SeedCOA(context.Background(), db, bTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	acctID := func(code string) uint64 {
		var id uint64
		db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = ?`, bTenant, code).Scan(&id)
		if id == 0 {
			t.Fatalf("akun %s tidak ada (SeedCOA belum jalan?)", code)
		}
		return id
	}

	// 1) Jurnal kapitalisasi RAB historis (data lama, sebelum rule dicabut):
	//    Dr 1-3100 100jt / Cr 2-1000 100jt, TANPA baris cost_entries. Source
	//    "rab_capitalization" mencerminkan budget.capitalizationJournalSource
	//    (unexported, tidak bisa diimpor dari budget_test).
	capJ := bSeedJournalWithSource(t, db, true, "rab_capitalization")
	for _, ln := range []struct {
		code          string
		debit, credit int64
	}{{"1-3100", 100_000_000, 0}, {"2-1000", 0, 100_000_000}} {
		if err := db.Exec(`INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit, project_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,NOW(3),NOW(3))`,
			bTenant, capJ, acctID(ln.code), domain.FromInt(ln.debit), domain.FromInt(ln.credit), project).Error; err != nil {
			t.Fatalf("seed kapitalisasi line %s: %v", ln.code, err)
		}
	}

	// 2) Transaksi aktual 30jt (rule baru): cost.Service.plan() SELALU mendebit
	//    Persediaan (1-3100) langsung — tidak ada lagi redirect ke Hutang Usaha.
	actJ := bSeedJournal(t, db, true)
	for _, ln := range []struct {
		code          string
		debit, credit int64
	}{{"1-3100", 30_000_000, 0}, {"1-1300", 0, 30_000_000}} {
		if err := db.Exec(`INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit, project_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,NOW(3),NOW(3))`,
			bTenant, actJ, acctID(ln.code), domain.FromInt(ln.debit), domain.FromInt(ln.credit), project).Error; err != nil {
			t.Fatalf("seed aktual line %s: %v", ln.code, err)
		}
	}
	if err := db.Exec(`INSERT INTO cost_entries (tenant_id, project_id, category, cost_tier, amount, payment_method, bank_account_code, date, vendor, description, journal_entry_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,NOW(3),NOW(3))`,
		bTenant, project, "hard", "shared", domain.FromInt(30_000_000), "bank", "1-1300", "2026-07-01", "V", "", actJ).Error; err != nil {
		t.Fatalf("seed cost aktual: %v", err)
	}

	prov := budget.NewGORMRealisasiProvider(db)
	got, err := prov.GetRealisasiByProject(context.Background(), bTenant, project, nil)
	if err != nil {
		t.Fatalf("GetRealisasiByProject: %v", err)
	}
	// Realisasi = hanya 30jt aktual, BUKAN 100jt (kapitalisasi historis) atau
	// 130jt (jumlah keduanya).
	if v := got[domain.CostCategoryHard]; v.String() != "30000000" {
		t.Errorf("hard realisasi: got %s, want 30000000 (jurnal kapitalisasi historis tidak boleh ikut terhitung sbg realisasi)", v.String())
	}
}

// Increment 2: realisasi marketing/other mengalir dari cost entry tier overhead
// (posted-only tetap berlaku — draft marketing tidak dihitung).
func TestIntegration_Realisasi_MarketingOther_ViaOverhead(t *testing.T) {
	db := bConnect(t)
	bCleanup(t, db)
	defer bCleanup(t, db)

	const project = uint64(556)
	bSeedProject(t, db, project)
	if err := ledger.SeedCOA(context.Background(), db, bTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	postedJ := bSeedJournal(t, db, true)
	draftJ := bSeedJournal(t, db, false)
	bSeedCost(t, db, project, postedJ, "marketing", 30_000_000) // POSTED → dihitung
	bSeedCost(t, db, project, draftJ, "marketing", 10_000_000)  // DRAFT → TIDAK
	bSeedCost(t, db, project, postedJ, "other", 5_000_000)      // POSTED other

	prov := budget.NewGORMRealisasiProvider(db)
	got, err := prov.GetRealisasiByProject(context.Background(), bTenant, project, nil)
	if err != nil {
		t.Fatalf("GetRealisasiByProject: %v", err)
	}
	if v := got[domain.CostCategoryMarketing]; v.String() != "30000000" {
		t.Errorf("marketing realisasi: got %s, want 30000000 (draft dikecualikan)", v.String())
	}
	if v := got[domain.CostCategoryOther]; v.String() != "5000000" {
		t.Errorf("other realisasi: got %s, want 5000000", v.String())
	}
}

// ── Bug #2 (cash-basis realisasi) ──────────────────────────────────────────
//
// Keputusan klien (AskUserQuestion, sesi ini): realisasi RAB vs Realisasi
// dihitung basis KAS khusus untuk laporan ini — cost entry yang lahir dari
// tagihan vendor (AP invoice) diprorata sebesar fraksi yang SUDAH DIBAYAR,
// bukan nilai penuh yang diakui saat tagihan diposting (akrual). Ledger SSOT
// (GetRealisasiByProject di atas, dipakai HPP/closing/alokasi) tidak berubah.

func bSeedVendor(t *testing.T, db *gorm.DB, name string) uint64 {
	t.Helper()
	res := db.Exec(`INSERT INTO vendors (tenant_id, name, is_pkp, is_active, created_at, updated_at)
		VALUES (?,?,0,1,NOW(3),NOW(3))`, bTenant, name)
	if res.Error != nil {
		t.Fatalf("seed vendor: %v", res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

// bSeedAPInvoice menyemai ap_invoices dengan dpp_amount == payable_amount
// (tanpa PPN/retensi/uang muka) — cukup untuk menguji proporsi pembayaran.
func bSeedAPInvoice(t *testing.T, db *gorm.DB, vendorID, journalID uint64, amount int64) uint64 {
	t.Helper()
	res := db.Exec(`INSERT INTO ap_invoices
		(tenant_id, vendor_id, invoice_number, invoice_date, due_date,
		 dpp_amount, ppn_amount, retention_amount, advance_applied, payable_amount,
		 status, journal_entry_id, posted_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,0,0,0,?,?,?,NOW(3),NOW(3),NOW(3))`,
		bTenant, vendorID, "INV-TEST", "2026-07-01", "2026-08-01",
		domain.FromInt(amount), domain.FromInt(amount), "posted", journalID)
	if res.Error != nil {
		t.Fatalf("seed ap_invoice: %v", res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

// bSeedAllocation menyemai satu pembayaran kas ke vendor (ap_payments) DAN
// alokasinya ke tagihan (ap_payment_allocations, allocation_type='invoice') —
// chk_apa_origin mewajibkan baris 'invoice' menunjuk payment_id yang nyata.
func bSeedAllocation(t *testing.T, db *gorm.DB, vendorID, journalID, invoiceID uint64, amount int64) {
	t.Helper()
	res := db.Exec(`INSERT INTO ap_payments
		(tenant_id, vendor_id, payment_kind, payment_date, amount, cash_account_code, journal_entry_id, created_at, updated_at)
		VALUES (?,?,'invoice',?,?,?,?,NOW(3),NOW(3))`,
		bTenant, vendorID, "2026-07-15", domain.FromInt(amount), "1-1300", journalID)
	if res.Error != nil {
		t.Fatalf("seed ap_payment: %v", res.Error)
	}
	var paymentID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&paymentID)

	if err := db.Exec(`INSERT INTO ap_payment_allocations
		(tenant_id, payment_id, invoice_id, allocation_type, amount, created_at, updated_at)
		VALUES (?,?,?,'invoice',?,NOW(3),NOW(3))`,
		bTenant, paymentID, invoiceID, domain.FromInt(amount)).Error; err != nil {
		t.Fatalf("seed ap_payment_allocation: %v", err)
	}
}

// bSeedAPCost menyemai cost_entries dari tagihan vendor (ap_invoice_id
// terisi) — tier "shared" (pool proyek, tanpa unit), sama seperti bSeedCost.
func bSeedAPCost(t *testing.T, db *gorm.DB, projectID, journalID, invoiceID uint64, cat string, amount int64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO cost_entries
		(tenant_id, project_id, category, cost_tier, amount, payment_method, ap_invoice_id, date, vendor, description, journal_entry_id, created_at, updated_at)
		VALUES (?,?,?,'shared',?,'payable',?,?,?,?,?,NOW(3),NOW(3))`,
		bTenant, projectID, cat, domain.FromInt(amount), invoiceID, "2026-07-01", "V", "", journalID).Error; err != nil {
		t.Fatalf("seed ap cost: %v", err)
	}
}

// bSeedCashCost menyemai cost_entries kas langsung (ap_invoice_id NULL),
// opsional bertaut ke budget_item_id — dipakai uji hierarki Konstruksi.
func bSeedCashCost(t *testing.T, db *gorm.DB, projectID, journalID uint64, cat string, amount int64, budgetItemID uint64) {
	t.Helper()
	var itemArg interface{}
	if budgetItemID != 0 {
		itemArg = budgetItemID
	}
	if err := db.Exec(`INSERT INTO cost_entries
		(tenant_id, project_id, category, cost_tier, amount, payment_method, bank_account_code, budget_item_id, date, vendor, description, journal_entry_id, created_at, updated_at)
		VALUES (?,?,?,'shared',?,'bank','1-1300',?,?,?,?,?,NOW(3),NOW(3))`,
		bTenant, projectID, cat, domain.FromInt(amount), itemArg, "2026-07-01", "V", "", journalID).Error; err != nil {
		t.Fatalf("seed cash cost: %v", err)
	}
}

// TestIntegration_RealisasiCash_APInvoicePartialPayment membuktikan skenario
// bug klien persis: RAB Tanah 100jt, tagihan vendor 100jt SUDAH diposting
// (akrual 100%), tapi baru dibayar sebagian → realisasi KAS harus mengikuti
// proporsi pembayaran (0% / 50% / 100%), bukan langsung 100% saat tagihan
// diposting.
func TestIntegration_RealisasiCash_APInvoicePartialPayment(t *testing.T) {
	db := bConnect(t)
	bCleanup(t, db)
	defer bCleanup(t, db)

	if err := ledger.SeedCOA(context.Background(), db, bTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	vendorID := bSeedVendor(t, db, "Vendor Tanah")
	prov := budget.NewGORMRealisasiProvider(db)

	cases := []struct {
		project uint64
		paid    int64
		want    string
	}{
		{560, 0, "0"},                // RAB 100jt, dibayar 0 → realisasi kas 0%
		{561, 50_000_000, "50000000"},  // dibayar 50jt → realisasi kas 50%
		{562, 100_000_000, "100000000"}, // dibayar 100jt → realisasi kas 100%
	}
	for _, tc := range cases {
		bSeedProject(t, db, tc.project)
		j := bSeedJournal(t, db, true)
		invID := bSeedAPInvoice(t, db, vendorID, j, 100_000_000)
		bSeedAPCost(t, db, tc.project, j, invID, "land", 100_000_000)
		if tc.paid > 0 {
			bSeedAllocation(t, db, vendorID, j, invID, tc.paid)
		}
		got, err := prov.GetRealisasiByProjectCash(context.Background(), bTenant, tc.project, nil)
		if err != nil {
			t.Fatalf("project %d: GetRealisasiByProjectCash: %v", tc.project, err)
		}
		if v := got[domain.CostCategoryLand]; v.String() != tc.want {
			t.Errorf("project %d (dibayar %d): land realisasi kas = %s, want %s", tc.project, tc.paid, v.String(), tc.want)
		}
	}
}

// TestIntegration_ConstructionRealisasiTree_SumNotAverage membuktikan skenario
// verifikasi FE yang diminta klien: hierarki Konstruksi→Subkategori→Item,
// dengan total parent (Sarana & Prasarana) = SUM Budgeted/Realisasi anak-anaknya
// lalu persentase dihitung dari total tersebut — BUKAN rata-rata persentase
// anak (rata-rata (50+25+0)/3=25% berbeda dari SUM 50/180=27.78%).
func TestIntegration_ConstructionRealisasiTree_SumNotAverage(t *testing.T) {
	db := bConnect(t)
	bCleanup(t, db)
	defer bCleanup(t, db)

	const project = uint64(563)
	bSeedProject(t, db, project)
	if err := ledger.SeedCOA(context.Background(), db, bTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}

	store := budget.NewGORMRepository(db)
	prov := budget.NewGORMRealisasiProvider(db)
	svc := budget.NewService(store, prov)
	ctx := context.Background()

	plan, err := svc.CreatePlan(ctx, bTenant, budget.CreatePlanRequest{ProjectID: project, Label: "RAB Test Konstruksi"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}

	addItem := func(sub string, desc string, amount int64) *budget.BudgetItem {
		item, err := svc.AddItem(ctx, bTenant, budget.AddItemRequest{
			PlanID:         plan.ID,
			Category:       budget.BudgetCategoryConstruction,
			Subcategory:    sub,
			Description:    desc,
			BudgetedAmount: domain.FromInt(amount),
		})
		if err != nil {
			t.Fatalf("AddItem %s: %v", desc, err)
		}
		return item
	}
	pondasi := addItem(string(domain.ConstructionProduksiSubsidi), "Pondasi", 100_000_000)
	pbg := addItem(string(domain.ConstructionPerizinan), "PBG", 30_000_000)
	aspal := addItem(string(domain.ConstructionSaranaPrasarana), "Aspal", 80_000_000)
	taman := addItem(string(domain.ConstructionSaranaPrasarana), "Taman", 40_000_000)
	tembok := addItem(string(domain.ConstructionSaranaPrasarana), "Tembok", 60_000_000)

	if _, err := svc.ApprovePlan(ctx, bTenant, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "test"}); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	j := bSeedJournal(t, db, true)
	bSeedCashCost(t, db, project, j, "hard", 50_000_000, pondasi.ID) // Pondasi 100jt/50jt
	bSeedCashCost(t, db, project, j, "hard", 40_000_000, aspal.ID)   // Aspal 80jt/40jt
	bSeedCashCost(t, db, project, j, "hard", 10_000_000, taman.ID)   // Taman 40jt/10jt
	// PBG dan Tembok sengaja TIDAK diberi cost entry (realisasi = 0).
	_ = pbg
	_ = tembok

	tree, err := svc.GetConstructionRealisasiTree(ctx, bTenant, project, nil)
	if err != nil {
		t.Fatalf("GetConstructionRealisasiTree: %v", err)
	}

	var sarpras *budget.SubcategoryRealisasiGroup
	for _, g := range tree.Groups {
		if g.Subcategory == domain.ConstructionSaranaPrasarana {
			sarpras = g
		}
	}
	if sarpras == nil {
		t.Fatalf("grup Sarana & Prasarana tidak ditemukan dalam tree: %+v", tree.Groups)
	}
	// SUM anak: Aspal 80/40, Taman 40/10, Tembok 60/0 → 180jt budget, 50jt realisasi.
	if sarpras.Budgeted != "180000000" {
		t.Errorf("Sarpras Budgeted = %s, want 180000000 (SUM anak, bukan rata-rata)", sarpras.Budgeted)
	}
	if sarpras.Realisasi != "50000000" {
		t.Errorf("Sarpras Realisasi = %s, want 50000000 (SUM anak, bukan rata-rata)", sarpras.Realisasi)
	}
	// Persentase dari TOTAL (50/180=27.78%), BUKAN rata-rata (50%,25%,0%)/3=25%.
	if sarpras.PersenRealisasi != "27.78%" {
		t.Errorf("Sarpras PersenRealisasi = %s, want 27.78%% (dari total, bukan rata-rata child percent)", sarpras.PersenRealisasi)
	}

	byDesc := map[string]*budget.ItemRealisasiRow{}
	for _, row := range sarpras.Items {
		byDesc[row.Description] = row
	}
	if r := byDesc["Aspal"]; r == nil || r.Realisasi != "40000000" || r.PersenRealisasi != "50.00%" {
		t.Errorf("Aspal = %+v, want realisasi 40000000 / 50.00%%", r)
	}
	if r := byDesc["Taman"]; r == nil || r.Realisasi != "10000000" || r.PersenRealisasi != "25.00%" {
		t.Errorf("Taman = %+v, want realisasi 10000000 / 25.00%%", r)
	}
	if r := byDesc["Tembok"]; r == nil || r.Realisasi != "0" || r.PersenRealisasi != "0.00%" {
		t.Errorf("Tembok = %+v, want realisasi 0 / 0.00%%", r)
	}

	var perizinan *budget.SubcategoryRealisasiGroup
	for _, g := range tree.Groups {
		if g.Subcategory == domain.ConstructionPerizinan {
			perizinan = g
		}
	}
	if perizinan == nil || len(perizinan.Items) != 1 || perizinan.Items[0].Description != "PBG" || perizinan.Items[0].Realisasi != "0" {
		t.Errorf("grup Perizinan tidak sesuai: %+v", perizinan)
	}

	// Total Konstruksi (grand total) juga SUM lintas subkategori, bukan
	// rata-rata: Pondasi 100/50 + PBG 30/0 + Sarpras 180/50 = 310jt/100jt.
	if tree.Budgeted != "310000000" || tree.Realisasi != "100000000" {
		t.Errorf("total Konstruksi = %s/%s, want 310000000/100000000", tree.Budgeted, tree.Realisasi)
	}
}
