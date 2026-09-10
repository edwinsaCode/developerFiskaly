//go:build integration

package reporting

// ═════════════════════════════════════════════════════════════════════════════
// FINAL ACCEPTANCE TEST — 8 aturan klien dalam SATU skenario proyek nyata.
//
// Skenario: satu proyek dengan tanah pool (Land HPP proporsional land_area,
// Item 9 — land_area disamakan 100 utk ketiga unit di fixture ini supaya
// split tetap 1/3 masing2 terlepas dari saleable_area yg berbeda + Kelebihan
// Tanah), tiga unit (Subsidi tunai terjual, Komersial KPR terjual, Komersial
// belum terjual/persediaan), HPP Konstruksi = Produksi+Sarana&Prasarana+
// Perizinan, KPR developer-borne bank fee (5-3200), dan reklas Piutang
// Bank (1-2200) → lunas saat pencairan (T-3).
//
// Angka expected dihitung TANGAN (lihat komentar di setiap langkah) SEBELUM
// skenario dijalankan lewat service produksi. Lima checkpoint dibandingkan:
// expected vs jurnal aktual vs P&L vs Neraca vs Persediaan.
//
// RULE KLIEN FREEZE (2026-09-04): HPP/Persediaan hanya Tanah + Konstruksi/Hard
// Cost. Soft Cost dan Operasional (dahulu "Pendanaan") BUKAN HPP walau ada di
// RAB — direalisasikan langsung sebagai Beban periode (5-4700/5-4600) saat
// biaya terjadi, tidak pernah menyentuh Persediaan (1-3200/1-3300 dibekukan
// LEGACY, lihat migrasi 000099). Skenario ini sudah direkalkulasi mengikuti
// aturan itu — checkpoint 1 dan 4 secara eksplisit memverifikasi invariant
// "kategori non-HPP tidak pernah masuk Persediaan/HPP".
//
// Jalankan: TEST_DB_DSN=... go test -tags integration ./internal/reporting/... -run TestAcceptance -v
// ═════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/billing"
	"esaproperti/internal/budget"
	"esaproperti/internal/customer"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
	"esaproperti/internal/sale"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
	"esaproperti/internal/tax"
)

const asTenant uint64 = 9_900_888

func asCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`UPDATE land_stock_reservations SET converted_sale_id = NULL WHERE tenant_id = ?`, asTenant).Error; err != nil {
		t.Fatalf("cleanup putus siklus land_stock_reservations: %v", err)
	}
	for _, tbl := range []string{
		"land_allocations", "land_sales", "land_stock_reservations", "land_stock",
		"tax_payments", "tax_obligations", "tax_rates",
		"contract_payment_events", "payment_schedules", "sale_contracts",
		"documents", "document_sequences", "document_types",
		"receipts", "receipt_sequences", "credit_applications", "payment_allocations",
		"invoices", "bookings", "sale_records", "termin_payments",
		"journal_lines", "journal_entries",
		"allocation_snapshot_lines", "allocation_snapshots",
		"allocation_config_versions", "allocation_configs", "allocation_executions",
		"budget_items", "budget_plans",
		"payment_schemes", "financing_sources",
		"unit_status_transitions", "units", "product_types",
		"project_progress_entries", "project_phases", "projects",
		"sales_persons", "customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", asTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

type asEnv struct {
	db        *gorm.DB
	svc       *sale.Service
	schemeSvc *scheme.Service
	budgetSvc *budget.Service
	taxSvc    *tax.Service
	ledgerQ   *ledger.QueryService
	repSvc    *Service
	landSvc   *land.Service
	posting   *ledger.PostingService

	projectID   uint64
	customerID  uint64
	salesID     uint64
	finSourceID uint64
	schemeKPRID uint64
	planID      uint64

	unitA uint64 // subsidi, tunai, terjual
	unitB uint64 // komersial, KPR, terjual
	unitC uint64 // komersial, belum terjual (persediaan)
}

func asSetup(t *testing.T) *asEnv {
	t.Helper()
	db := eqConnect(t)
	asCleanup(t, db)
	t.Cleanup(func() { asCleanup(t, db) })
	ctx := context.Background()

	if err := ledger.SeedCOA(ctx, db, asTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	if err := scheme.SeedDefaultSchemes(ctx, db, asTenant); err != nil {
		t.Fatalf("seed schemes: %v", err)
	}
	if err := document.SeedDefaultDocumentTypes(ctx, db, asTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	if err := tax.SeedDefaultRates(ctx, db, asTenant); err != nil {
		t.Fatalf("seed tax rates: %v", err)
	}

	// Product Catalog: Subsidi (no PPN) & Komersial (PPN 11%), rule klien
	// (tax_category menentukan tarif PPh Final: subsidi 1%, komersial 2,5%).
	if err := db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, tax_category, is_active)
		VALUES (?,?,?,?,?,?,TRUE)`, asTenant, "rumah_subsidi", "Rumah Subsidi", "property", "4-1000", "subsidi").Error; err != nil {
		t.Fatalf("seed product_type subsidi: %v", err)
	}
	if err := db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, tax_category, is_active)
		VALUES (?,?,?,?,?,?,TRUE)`, asTenant, "rumah_komersial", "Rumah Komersial", "property", "4-1000", "komersial").Error; err != nil {
		t.Fatalf("seed product_type komersial: %v", err)
	}

	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).
		WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo, allocation.WithLandPoolSource(allocRepo))
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)

	billingRepo := billing.NewGORMRepository(db)
	receiptSvc := billing.NewReceiptService(billingRepo, billingRepo, billingRepo)
	saleRepo.SetReceiptTxGenerator(&eqReceiptGen{svc: receiptSvc})

	custRepo := customer.NewGORMRepository(db)
	personRepo := salesorg.NewGORMRepository(db)

	budgetRepo := budget.NewGORMRepository(db)
	budgetSvc := budget.NewService(budgetRepo, budget.NewGORMRealisasiProvider(db))

	taxRepo := tax.NewGORMRepository(db, posting)
	taxSvc := tax.NewService(taxRepo, taxRepo, taxRepo, taxRepo,
		tax.WithBASTReader(taxRepo), tax.WithLedgerReader(taxRepo),
		tax.WithRuleResolution(taxRepo, taxRepo), tax.WithUnitProductPolicy(taxRepo))

	landRepo := land.NewGORMRepository(db)
	landHPPResolver := land.NewPurchasePriceLandHPPResolver(landRepo)
	// PPh Final Pengalihan Kelebihan Tanah — engine yang SAMA dipakai unit/BAST
	// (taxSvc di atas), otomatis ter-accrue di dalam RecordAkadTx milik land,
	// bukan lagi taxSvc.AccrueTax manual (fix "GAP TERBUKA" versi sebelumnya).
	landSvc := land.NewService(landRepo, land.WithHPPResolver(landHPPResolver), land.WithPPhResolver(taxSvc))

	hppResolver := sale.NewBudgetedHPPResolver(budgetSvc, allocSvc, saleRepo)

	svc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo), sale.WithPaymentCommitter(saleRepo),
		sale.WithBookingStore(saleRepo),
		sale.WithSchemeFlow(saleRepo, scheme.DefaultRegistry(), &eqParties{customers: custRepo, persons: personRepo}),
		sale.WithHouseARStore(saleRepo),
		sale.WithHouseARLedger(ledger.NewLedgerBalanceService(ledger.NewQueryService(db))),
		sale.WithHPPResolver(hppResolver),
		sale.WithLandAkadPreparer(landSvc))
	svc.SetProductPolicyResolver(project.NewGORMRepository(db))

	schemeSvc := scheme.NewService(scheme.NewGORMRepository(db), scheme.DefaultRegistry())

	ledgerQ := ledger.NewQueryService(db)
	repRepo := NewGORMRepository(db)
	repSvc := NewService(ledgerQ, repRepo, repRepo, repRepo).WithTaxReader(taxSvc).WithHouseReader(svc)

	env := &asEnv{
		db: db, svc: svc, schemeSvc: schemeSvc, budgetSvc: budgetSvc, taxSvc: taxSvc,
		ledgerQ: ledgerQ, repSvc: repSvc, landSvc: landSvc, posting: posting,
	}

	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		asTenant, "Akseptansi", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.projectID)

	if err := db.Exec(`INSERT INTO allocation_configs (tenant_id, project_id, basis) VALUES (?,?,?)`,
		asTenant, env.projectID, "saleable_area").Error; err != nil {
		t.Fatalf("seed allocation config: %v", err)
	}

	cust, err := customer.NewService(custRepo).Create(ctx, asTenant, customer.CreateCustomerRequest{Code: "AS-C1", Name: "Andi"})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	env.customerID = cust.ID

	person := &salesorg.SalesPerson{TenantID: asTenant, Code: "AS-S1", Name: "Dewi", IsActive: true}
	if err := personRepo.CreatePerson(ctx, person); err != nil {
		t.Fatalf("seed sales person: %v", err)
	}
	env.salesID = person.ID

	fs, err := schemeSvc.CreateFinancingSource(ctx, asTenant, scheme.CreateFinancingSourceRequest{
		Code: "ASBTN", Name: "Bank BTN", Type: scheme.FinSourceKPRKomersial,
	})
	if err != nil {
		t.Fatalf("seed financing source: %v", err)
	}
	env.finSourceID = fs.ID

	schemes, err := schemeSvc.ListSchemes(ctx, asTenant)
	if err != nil {
		t.Fatalf("list schemes: %v", err)
	}
	for _, m := range schemes {
		if m.Code == "KPR-KOM" {
			env.schemeKPRID = m.ID
		}
	}
	if env.schemeKPRID == 0 {
		t.Fatal("skema KPR-KOM tidak ditemukan di seed")
	}

	// ── RAB: Land 350jt + Construction (Produksi 400jt + Sarana&Prasarana 150jt
	//    + Perizinan 50jt = 600jt) + Soft 90jt + Operasional 30jt = 1.070.000.000.
	//    Soft & Operasional TETAP baris RAB (rencana anggaran), tapi BUKAN pool
	//    HPP — realisasinya nanti jadi Beban periode, bukan Persediaan.
	plan, err := budgetSvc.CreatePlan(ctx, asTenant, budget.CreatePlanRequest{ProjectID: env.projectID, Label: "RAB v1"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	env.planID = plan.ID
	items := []budget.AddItemRequest{
		{PlanID: plan.ID, Category: budget.BudgetCategoryLand, Subcategory: "tanah", Description: "Tanah", BudgetedAmount: domain.FromInt(350_000_000)},
		{PlanID: plan.ID, Category: budget.BudgetCategoryConstruction, Subcategory: "produksi", Description: "Produksi", BudgetedAmount: domain.FromInt(400_000_000)},
		{PlanID: plan.ID, Category: budget.BudgetCategoryConstruction, Subcategory: "sarana_prasarana", Description: "Sarana & Prasarana", BudgetedAmount: domain.FromInt(150_000_000)},
		{PlanID: plan.ID, Category: budget.BudgetCategoryConstruction, Subcategory: "perizinan", Description: "Perizinan (IMB/SLF)", BudgetedAmount: domain.FromInt(50_000_000)},
		{PlanID: plan.ID, Category: budget.BudgetCategorySoft, Subcategory: "desain_legal", Description: "Desain & Legal", BudgetedAmount: domain.FromInt(90_000_000)},
		{PlanID: plan.ID, Category: budget.BudgetCategoryOperational, Subcategory: "operasional", Description: "Beban Operasional (dahulu 'Pendanaan')", BudgetedAmount: domain.FromInt(30_000_000)},
	}
	for _, it := range items {
		if _, err := budgetSvc.AddItem(ctx, asTenant, it); err != nil {
			t.Fatalf("AddItem %s/%s: %v", it.Category, it.Subcategory, err)
		}
	}
	if _, err := budgetSvc.ApprovePlan(ctx, asTenant, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "AS-S1"}); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}

	// Biaya aktual dikapitalisasi PERSIS sebesar RAB (proyek "on-budget") —
	// Dr Persediaan / Cr Hutang Usaha, project-tagged. HANYA Land+Hard yang
	// dikapitalisasi (RULE KLIEN FREEZE 2026-09-04). Soft & Operasional
	// direalisasikan LANGSUNG sebagai Beban periode (Dr 5-4700/5-4600) saat
	// biaya terjadi — TIDAK PERNAH menyentuh Persediaan 1-3200/1-3300.
	env.postCost(t, "1-3000", 350_000_000)
	env.postCost(t, "1-3100", 600_000_000)
	env.postCost(t, "5-4700", 90_000_000) // Beban Soft Cost — realisasi periode, bukan Persediaan
	env.postCost(t, "5-4600", 30_000_000) // Beban Operasional — realisasi periode, bukan Persediaan

	// Land pool Kelebihan Tanah: carve-out tetap 50m² x 1.000.000 = 50.000.000
	// dari 1-3000 (sisanya, 300jt, rata ke 3 unit = 100jt/unit — rule klien #1).
	if _, err := landSvc.CreatePool(ctx, asTenant, land.CreatePoolRequest{
		ProjectID: env.projectID, TotalQuantityM2: decimal.RequireFromString("50"),
		PurchasePrice: domain.FromInt(1_000_000), UnitPrice: domain.FromInt(1_600_000),
	}); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}

	// Unit saleable_area rasio 40:60:100 → Hard (satu-satunya pool non-land
	// yang masih dialokasikan per unit — Soft/Operasional sudah jadi Beban
	// periode, tidak dialokasikan per unit) terbagi 2:3:5 → A=120jt, B=180jt,
	// C=300jt. A/B "reserved" (siap Akad); C tetap "available" (belum
	// terjual — persediaan).
	env.unitA = env.seedUnit(t, "AS-A1", "rumah_subsidi", 40, "reserved")
	env.unitB = env.seedUnit(t, "AS-B1", "rumah_komersial", 60, "reserved")
	env.unitC = env.seedUnit(t, "AS-C1", "rumah_komersial", 100, "available")

	return env
}

func (e *asEnv) seedUnit(t *testing.T, code, unitType string, saleableArea int64, status string) uint64 {
	t.Helper()
	// land_area (Item 9): disamakan 100 utk semua unit (lepas dari
	// saleable_area) supaya Land HPP tetap terbagi rata 1/3 seperti yang
	// diharapkan checkpoint di test ini (100jt per unit dari pool 300jt).
	if err := e.db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		asTenant, e.projectID, code, unitType, domain.FromInt(saleableArea), domain.FromInt(100), domain.FromInt(500_000_000), status).Error; err != nil {
		t.Fatalf("seed unit %s: %v", code, err)
	}
	var id uint64
	e.db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func (e *asEnv) postCost(t *testing.T, accountCode string, amount int64) {
	t.Helper()
	ctx := context.Background()
	ids := e.accountIDs(t, accountCode, "2-1000")
	pid := e.projectID
	day := time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	entry, err := e.posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: asTenant, Date: day, Description: "Kapitalisasi biaya (akseptansi) " + accountCode,
		Lines: []ledger.LineInput{
			{AccountID: ids[accountCode], Debit: domain.FromInt(amount), ProjectID: &pid},
			{AccountID: ids["2-1000"], Credit: domain.FromInt(amount), ProjectID: &pid},
		},
	})
	if err != nil {
		t.Fatalf("cost journal %s: %v", accountCode, err)
	}
	if _, err := e.posting.Post(ctx, asTenant, entry.ID); err != nil {
		t.Fatalf("post cost journal %s: %v", accountCode, err)
	}
}

func (e *asEnv) accountIDs(t *testing.T, codes ...string) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for _, c := range codes {
		var id uint64
		e.db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = ?`, asTenant, c).Scan(&id)
		if id == 0 {
			t.Fatalf("akun %s tidak ada", c)
		}
		out[c] = id
	}
	return out
}

// accountBalance: saldo arah normal DEBIT (Dr - Cr) untuk satu akun, seluruh
// jurnal terposting tenant ini (tanpa filter unit) — dipakai untuk memverifikasi
// siklus 1-2200 (Dana Jaminan Bank) dan saldo Persediaan (akun aset).
func (e *asEnv) accountBalance(t *testing.T, code string) string {
	t.Helper()
	var v string
	err := e.db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit), 0) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE jl.tenant_id = ? AND a.code = ? AND je.posted_at IS NOT NULL`,
		asTenant, code).Scan(&v).Error
	if err != nil {
		t.Fatalf("balance query %s: %v", code, err)
	}
	m, _ := domain.NewMoney(v)
	return m.String()
}

// creditBalance: saldo arah normal KREDIT (Cr - Dr) — dipakai untuk akun
// kewajiban (2-xxxx).
func (e *asEnv) creditBalance(t *testing.T, code string) string {
	t.Helper()
	var v string
	err := e.db.Raw(`SELECT COALESCE(SUM(jl.credit - jl.debit), 0) FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		WHERE jl.tenant_id = ? AND a.code = ? AND je.posted_at IS NOT NULL`,
		asTenant, code).Scan(&v).Error
	if err != nil {
		t.Fatalf("balance query %s: %v", code, err)
	}
	m, _ := domain.NewMoney(v)
	return m.String()
}

// ═════════════════════════════════════════════════════════════════════════════
// TestAcceptance_FullScenario_8Rules
// ═════════════════════════════════════════════════════════════════════════════
func TestAcceptance_FullScenario_8Rules(t *testing.T) {
	env := asSetup(t)
	ctx := context.Background()
	day := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)

	// ── Unit A — Subsidi, Tunai, no PPN, PPh Final 1% ──────────────────────────
	// Ekspektasi tangan: DPP=300.000.000, HPP = Land100 + Hard120 = 220.000.000
	// (RULE KLIEN FREEZE 2026-09-04: Soft/Operasional bukan HPP lagi).
	// PPh Final Subsidi 1% x 300jt = 3.000.000.
	if _, err := env.svc.ReceivePayment(ctx, asTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, UnitID: &env.unitA,
		Amount: domain.FromInt(300_000_000), Date: day, BankAccountCode: "1-1300",
	}); err != nil {
		t.Fatalf("bayar tunai unit A: %v", err)
	}
	recA, err := env.svc.RecordAkad(ctx, asTenant, sale.RecordBASTRequest{
		UnitID: env.unitA, SalePrice: domain.FromInt(300_000_000), IsVAT: false,
		BuyerRef: "Andi (Subsidi)", BASTDate: day,
	})
	if err != nil {
		t.Fatalf("RecordAkad unit A: %v", err)
	}
	if _, err := env.taxSvc.AccrueTax(ctx, asTenant, tax.AccrueTaxRequest{
		RateCode: "pph_final_pengalihan", TransferValue: domain.FromInt(300_000_000),
		AccrualDate: day, UnitID: &env.unitA, ProjectID: &env.projectID,
	}); err != nil {
		t.Fatalf("AccrueTax unit A: %v", err)
	}

	// ── Unit B — Komersial, KPR, PPN 11%, PPh Final 2,5% ───────────────────────
	// Ekspektasi tangan: DPP=500.000.000, PPN=55.000.000, Gross=555.000.000.
	// HPP = Land100 + Hard180 = 280.000.000 (RULE KLIEN FREEZE 2026-09-04:
	// Soft/Operasional bukan HPP lagi). PPh Final Komersial 2,5% x 500jt = 12.500.000.
	// Urutan KRITIS: DP → 3 event skema (state=Akad) → RecordAkad (BAST) SAAT
	// pencairan BELUM terjadi (totalAdvance=DP < gross) → sisa 500jt masuk
	// 1-2200 Dana Jaminan Bank (rule klien T-3/#4). Baru SETELAH itu pencairan
	// KPR diterima — masih dalam state Akad → mengkredit 1-2200 (bukan 1-2000),
	// melunasinya persis ke nol.
	contractB, err := env.svc.CreateContract(ctx, asTenant, sale.CreateContractRequest{
		UnitID: env.unitB, BuyerName: "Citra (Komersial)", BuyerID: "3271...",
		ContractDate: day, TotalPrice: domain.FromInt(500_000_000),
		IsPKP: true, VATRateSnapshot: decimal.RequireFromString("0.11"),
		PaymentSchemeID: &env.schemeKPRID, FinancingSourceID: &env.finSourceID,
		CustomerID: &env.customerID, SalesPersonID: &env.salesID,
	})
	if err != nil {
		t.Fatalf("CreateContract unit B: %v", err)
	}
	cidB := contractB.ID
	if _, err := env.svc.ReceivePayment(ctx, asTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cidB,
		Amount: domain.FromInt(55_000_000), Date: day, BankAccountCode: "1-1300",
	}); err != nil {
		t.Fatalf("DP unit B: %v", err)
	}
	for _, ev := range []scheme.Event{scheme.EventSubmittedToBank, scheme.EventBankApproved, scheme.EventAkad} {
		if _, err := env.svc.ApplySchemeEvent(ctx, asTenant, sale.ApplySchemeEventRequest{
			ContractID: cidB, Event: ev, EventDate: day, FinancingSourceID: &env.finSourceID,
		}); err != nil {
			t.Fatalf("ApplySchemeEvent %s unit B: %v", ev, err)
		}
	}
	bankApprovedB := domain.FromInt(500_000_000)
	recB, err := env.svc.RecordAkad(ctx, asTenant, sale.RecordBASTRequest{
		UnitID: env.unitB, SalePrice: domain.FromInt(500_000_000), IsVAT: true,
		VATRate: decimal.RequireFromString("0.11"), BuyerRef: "Citra (Komersial)", BASTDate: day,
		// Item 7A (UAT 2026-09-07): Nilai Persetujuan KPR Bank wajib diisi
		// sebelum Akad untuk kontrak KPR-financed.
		BankApprovedAmount: &bankApprovedB,
	})
	if err != nil {
		t.Fatalf("RecordAkad unit B: %v", err)
	}
	// Checkpoint intermediate: 1-2200 harus benar-benar terisi 500jt di sini —
	// bukti hidup siklus "Dana Jaminan Bank" sebelum pencairan.
	if got := env.accountBalance(t, "1-2200"); got != "500000000" {
		t.Fatalf("1-2200 pasca-Akad (sebelum pencairan) = %s, want 500000000", got)
	}
	if _, err := env.svc.ReceivePayment(ctx, asTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceKPRDisbursement, ContractID: &cidB,
		Amount: domain.FromInt(500_000_000), BankFee: domain.FromInt(5_000_000),
		Date: day, BankAccountCode: "1-1300", FinancingSourceID: &env.finSourceID,
	}); err != nil {
		t.Fatalf("pencairan KPR unit B: %v", err)
	}
	if _, err := env.taxSvc.AccrueTax(ctx, asTenant, tax.AccrueTaxRequest{
		RateCode: "pph_final_pengalihan", TransferValue: domain.FromInt(500_000_000),
		AccrualDate: day, UnitID: &env.unitB, ProjectID: &env.projectID,
	}); err != nil {
		t.Fatalf("AccrueTax unit B: %v", err)
	}

	// ── Kelebihan Tanah — standalone, DPP=80jt, PPN 11%, HPP=harga beli ────────
	// Ekspektasi tangan: 50m² x 1.600.000 = 80.000.000 DPP, PPN 8.800.000,
	// gross 88.800.000. HPP = 50m² x 1.000.000 (harga beli) = 50.000.000.
	// PPh Final Komersial (tax_category proyek "Akseptansi" default 'komersial')
	// 2,5% x 80jt = 2.000.000 — kini ter-accrue OTOMATIS di dalam RecordAkad
	// (land.WithPPhResolver), bukan taxSvc.AccrueTax manual.
	landSale, err := env.landSvc.RecordAkad(ctx, asTenant, land.RecordAkadRequest{
		ProjectID: env.projectID, CustomerID: env.customerID, SalesPersonID: &env.salesID,
		QuantityM2: decimal.RequireFromString("50"), DPPAmount: domain.FromInt(80_000_000),
		IsPKP: true, VATRateSnapshot: decimal.RequireFromString("0.11"),
		PaymentAccountCode: "1-1300", RecognitionDate: day,
	})
	if err != nil {
		t.Fatalf("RecordAkad Kelebihan Tanah: %v", err)
	}

	// ═══════════════════════════════════════════════════════════════════════
	// CHECKPOINT 1 — Jurnal aktual: HPP per unit persis sesuai hand-calc.
	// ═══════════════════════════════════════════════════════════════════════
	hppTotal := func(r *sale.SaleRecord) string {
		return r.HPPLand.Add(r.HPPHard).Add(r.HPPSoft).Add(r.HPPFinancing).String()
	}
	// REGRESSION (RULE KLIEN FREEZE 2026-09-04): HPPSoft/HPPFinancing wajib
	// NOL untuk setiap penjualan baru — kategori non-HPP tidak pernah boleh
	// masuk HPP walau dulu (legacy) field ini pernah terisi.
	if !recA.HPPSoft.IsZero() || !recA.HPPFinancing.IsZero() {
		t.Fatalf("unit A: HPPSoft/HPPFinancing harus nol (bukan HPP lagi), got Soft=%s Financing=%s", recA.HPPSoft, recA.HPPFinancing)
	}
	if !recB.HPPSoft.IsZero() || !recB.HPPFinancing.IsZero() {
		t.Fatalf("unit B: HPPSoft/HPPFinancing harus nol (bukan HPP lagi), got Soft=%s Financing=%s", recB.HPPSoft, recB.HPPFinancing)
	}
	moneyEq(t, "HPP unit A", hppTotal(recA), "220000000")
	moneyEq(t, "HPP unit B", hppTotal(recB), "280000000")

	var landCogs string
	if err := env.db.Table("journal_lines").
		Select("COALESCE(SUM(debit),0)").
		Where("tenant_id = ? AND journal_entry_id = ?", asTenant, *landSale.CogsJournalID).
		Scan(&landCogs).Error; err != nil {
		t.Fatalf("sum jurnal HPP tanah: %v", err)
	}
	moneyEq(t, "HPP Kelebihan Tanah", landCogs, "50000000")

	// PPh Final Kelebihan Tanah: jurnal otomatis, balanced, tertaut ke land_sales.
	if landSale.PPhJournalID == nil {
		t.Fatal("Kelebihan Tanah: pph_journal_id nil — PPh Final tidak ter-accrue otomatis")
	}
	var landPPhDebit, landPPhCredit string
	if err := env.db.Table("journal_lines").
		Select("COALESCE(SUM(debit),0)").
		Where("tenant_id = ? AND journal_entry_id = ?", asTenant, *landSale.PPhJournalID).
		Scan(&landPPhDebit).Error; err != nil {
		t.Fatalf("sum debit jurnal PPh tanah: %v", err)
	}
	if err := env.db.Table("journal_lines").
		Select("COALESCE(SUM(credit),0)").
		Where("tenant_id = ? AND journal_entry_id = ?", asTenant, *landSale.PPhJournalID).
		Scan(&landPPhCredit).Error; err != nil {
		t.Fatalf("sum credit jurnal PPh tanah: %v", err)
	}
	moneyEq(t, "PPh Final Kelebihan Tanah (debit)", landPPhDebit, "2000000")
	moneyEq(t, "PPh Final Kelebihan Tanah (credit)", landPPhCredit, "2000000")

	var landObligationCount int64
	if err := env.db.Table("tax_obligations").
		Where("tenant_id = ? AND land_sale_id = ?", asTenant, landSale.ID).
		Count(&landObligationCount).Error; err != nil {
		t.Fatalf("count tax_obligations tanah: %v", err)
	}
	if landObligationCount != 1 {
		t.Fatalf("tax_obligations utk land_sale_id=%d = %d, want 1 (no double-accrual)", landSale.ID, landObligationCount)
	}

	// 1-2200 harus kembali ke NOL persis (pencairan melunasi tepat sebesar sisa).
	moneyEq(t, "1-2200 akhir (Dana Jaminan Bank)", env.accountBalance(t, "1-2200"), "0")
	moneyEq(t, "1-2000 akhir (Piutang Customer)", env.accountBalance(t, "1-2000"), "0")
	moneyEq(t, "2-2000 akhir (Uang Muka Penjualan)", env.accountBalance(t, "2-2000"), "0")
	moneyEq(t, "2-3000 akhir (PPN Keluaran)", env.creditBalance(t, "2-3000"), "63800000")     // 55jt(B) + 8,8jt(tanah)
	moneyEq(t, "2-4000 akhir (Hutang PPh Final)", env.creditBalance(t, "2-4000"), "17500000") // 3jt(A) + 12,5jt(B) + 2jt(tanah)

	// ═══════════════════════════════════════════════════════════════════════
	// CHECKPOINT 2 — Laba Rugi konsolidasi.
	// ═══════════════════════════════════════════════════════════════════════
	pl, err := env.repSvc.GetKonsolidasiPL(ctx, asTenant, nil, day)
	if err != nil {
		t.Fatalf("GetKonsolidasiPL: %v", err)
	}
	moneyEq(t, "TotalPendapatan", pl.TotalPendapatan, "880000000") // 300+500(rumah) + 80(tanah)
	moneyEq(t, "TotalHPP", pl.TotalHPP, "550000000")               // 220+280(rumah, Land+Hard saja) + 50(tanah)
	moneyEq(t, "LabaKotor", pl.LabaKotor, "330000000")
	// TotalBebanOperasional kini juga menampung realisasi Soft Cost (5-4700)
	// dan Operasional (5-4600) — REGRESSION (RULE KLIEN FREEZE 2026-09-04):
	// keduanya Beban periode, bukan Persediaan/HPP.
	moneyEq(t, "TotalBebanOperasional", pl.TotalBebanOperasional, "125000000") // 5jt(bank fee KPR) + 90jt(Soft) + 30jt(Operasional)
	moneyEq(t, "LabaOperasional", pl.LabaOperasional, "205000000")
	moneyEq(t, "TotalBebanPajak", pl.TotalBebanPajak, "17500000") // 3jt(A) + 12,5jt(B) + 2jt(tanah)
	moneyEq(t, "LabaRugiBersih", pl.LabaRugiBersih, "187500000")

	// ═══════════════════════════════════════════════════════════════════════
	// CHECKPOINT 3 — Neraca: balanced persis.
	// ═══════════════════════════════════════════════════════════════════════
	neraca, err := env.repSvc.GetNeraca(ctx, asTenant, nil, day)
	if err != nil {
		t.Fatalf("GetNeraca: %v", err)
	}
	if !neraca.IsBalanced {
		t.Fatalf("Neraca TIDAK balanced: Aset=%s Kewajiban=%s Ekuitas=%s",
			neraca.TotalAset, neraca.TotalKewajiban, neraca.TotalEkuitasEfektif)
	}
	// RULE KLIEN FREEZE (2026-09-04): dibanding versi sebelum freeze —
	// Aset turun 60jt: Persediaan unit C kehilangan Soft45+Financing15 (lihat
	// Checkpoint 4, sekarang jadi Beban periode saat realisasi, bukan saat unit
	// C terjual). Kewajiban TIDAK berubah (Hutang Usaha 2-1000 sama persis,
	// hanya akun debit tujuannya yang beda). Ekuitas turun 60jt: TotalBebanOperasional
	// naik 120jt (Soft90+Operasional30 langsung jadi Beban), sebagian dikompensasi
	// TotalHPP turun 60jt (unit A+B tak lagi memikul porsi Soft/Financing) →
	// net LabaRugiBersih turun 60jt. Aset(-60) = Kewajiban(0) + Ekuitas(-60). ✓
	moneyEq(t, "TotalAset", neraca.TotalAset, "1338800000")
	moneyEq(t, "TotalKewajiban", neraca.TotalKewajiban, "1151300000") // +2jt Hutang PPh Final tanah vs versi sebelum fix
	moneyEq(t, "LabaRugiTahunBerjalan", neraca.LabaRugiTahunBerjalan, "187500000")
	moneyEq(t, "TotalEkuitasEfektif", neraca.TotalEkuitasEfektif, "187500000")
	moneyEq(t, "TotalKewajibanEkuitas", neraca.TotalKewajibanEkuitas, "1338800000")

	// ═══════════════════════════════════════════════════════════════════════
	// CHECKPOINT 4 — Persediaan tersisa: hanya unit C (belum terjual), HANYA
	// Land + Hard Cost (RULE KLIEN FREEZE 2026-09-04). Land 100jt + Hard 300jt
	// = 400jt. 1-3200/1-3300 dibekukan LEGACY — REGRESSION: keduanya wajib NOL
	// selamanya (kategori non-HPP tidak pernah masuk Persediaan/HPP), dan biaya
	// Soft/Operasional yang direalisasikan wajib muncul sebagai Beban periode
	// di 5-4700/5-4600, bukan di Persediaan.
	// ═══════════════════════════════════════════════════════════════════════
	moneyEq(t, "Persediaan 1-3000 (Tanah)", env.accountBalance(t, "1-3000"), "100000000")
	moneyEq(t, "Persediaan 1-3100 (Hard Cost)", env.accountBalance(t, "1-3100"), "300000000")
	moneyEq(t, "Persediaan 1-3200 (Soft Cost, LEGACY/BEKU — tidak boleh diisi lagi)", env.accountBalance(t, "1-3200"), "0")
	moneyEq(t, "Persediaan 1-3300 (Financing/Operasional, LEGACY/BEKU — tidak boleh diisi lagi)", env.accountBalance(t, "1-3300"), "0")
	moneyEq(t, "Beban Soft Cost 5-4700 (realisasi periode, bukan Persediaan)", env.accountBalance(t, "5-4700"), "90000000")
	moneyEq(t, "Beban Operasional 5-4600 (dahulu 'Pendanaan', realisasi periode)", env.accountBalance(t, "5-4600"), "30000000")

	// ═══════════════════════════════════════════════════════════════════════
	// CHECKPOINT 5 — Invariant #1: setiap jurnal yang tersentuh skenario ini
	// tetap balanced (Σdebit == Σkredit).
	// ═══════════════════════════════════════════════════════════════════════
	var unbalanced int64
	if err := env.db.Raw(`SELECT COUNT(*) FROM (
			SELECT je.id, SUM(jl.debit) d, SUM(jl.credit) c
			FROM journal_entries je JOIN journal_lines jl ON jl.journal_entry_id = je.id
			WHERE je.tenant_id = ? GROUP BY je.id HAVING d <> c
		) x`, asTenant).Scan(&unbalanced).Error; err != nil {
		t.Fatalf("cek balanced: %v", err)
	}
	if unbalanced != 0 {
		t.Fatalf("%d jurnal TIDAK balanced (Invariant #1 dilanggar)", unbalanced)
	}
}
