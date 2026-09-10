//go:build integration

package reporting

// ═════════════════════════════════════════════════════════════════════════════
// FINANCIAL CONSISTENCY SUITE (Phase 1 — docs/financial-canonical-registry.md)
//
// Test KESETARAAN ANGKA lintas modul: satu skenario bisnis nyata dijalankan
// lewat service produksi, lalu SETIAP angka keuangan dibandingkan antara jalur
// kanonik vs jalur pembaca lain (dashboard, kpr pipeline, statement, collection,
// receipts, ledger). Selisih Rp1 = GAGAL (perbandingan decimal persis).
//
// Phase 2 S1–S9 SELESAI: seluruh divergensi audit 2026-07-26 sudah disatukan —
// tidak ada lagi knownDivergence(); semua kesetaraan ditegakkan permanen
// (CONSISTENCY_STRICT tidak lagi mengubah perilaku suite).
//
// Jalankan: make test-consistency   (butuh MySQL dev; lihat Makefile)
// ═════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/allocation"
	"esaproperti/internal/billing"
	"esaproperti/internal/budget"
	"esaproperti/internal/cost"
	"esaproperti/internal/customer"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/notary"
	"esaproperti/internal/project"
	"esaproperti/internal/sale"
	"esaproperti/internal/salesorg"
	"esaproperti/internal/scheme"
	"esaproperti/internal/tax"
)

const eqTenant uint64 = 9_900_777

// ── Infrastruktur ─────────────────────────────────────────────────────────────

func eqConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati consistency suite")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func eqCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"notary_deposits",
		"refunds", "cancellations",
		"hpp_trueup_lines", "hpp_trueup_runs", "project_completion_events",
		"allocation_snapshot_lines", "allocation_snapshots", "sale_records",
		"credit_applications", "payment_allocations", "receipts", "receipt_sequences",
		"invoices", "bookings",
		"documents", "document_sequences", "document_types",
		"tax_payments", "tax_obligations", "tax_rates",
		"contract_payment_events", "payment_schedules", "sale_contracts",
		"journal_lines", "journal_entries",
		"allocation_config_versions", "allocation_configs", "allocation_executions",
		"budget_items", "budget_plans", "termin_payments",
		"payment_schemes", "financing_sources",
		"unit_status_transitions", "units", "product_types",
		"project_progress_entries", "project_phases", "projects",
		"sales_persons", "customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", eqTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

// Phase 2 SELESAI (S1–S9): seluruh knownDivergence() DIHAPUS — setiap
// kesetaraan di suite ini ditegakkan PERMANEN, tanpa mode skip.

// moneyEq: kesetaraan decimal PERSIS — selisih Rp1 (bahkan 0,0001) = gagal.
func moneyEq(t *testing.T, label, a, b string) {
	t.Helper()
	ma, err := domain.NewMoney(a)
	if err != nil {
		t.Fatalf("%s: nilai kiri bukan money: %q", label, a)
	}
	mb, err := domain.NewMoney(b)
	if err != nil {
		t.Fatalf("%s: nilai kanan bukan money: %q", label, b)
	}
	if !ma.Equal(mb) {
		t.Errorf("%s: TIDAK SETARA — %s vs %s", label, ma, mb)
	}
}

// ── Environment skenario ──────────────────────────────────────────────────────

type eqEnv struct {
	db        *gorm.DB
	svc       *sale.Service
	schemeSvc *scheme.Service
	budgetSvc *budget.Service
	taxSvc    *tax.Service
	ledgerQ   *ledger.QueryService
	repSvc    *Service
	dash      *dashboardQuerier
	kpr       *kprPipelineQuerier
	repRepo   *GORMRepository
	costRepo  costAccumulator
	posting   *ledger.PostingService

	projectID   uint64
	unitA       uint64 // dijual (booking→kontrak KPR→BAST)
	unitB       uint64 // booking hangus (forfeit → 4-2000)
	customerID  uint64
	salesID     uint64
	finSourceID uint64
	schemeKPRID uint64
	contractAID uint64
	planID      uint64
}

// costAccumulator: potongan kecil dari cost.GORMRepository yang kita pakai —
// di-declare lokal agar tidak menarik seluruh paket cost bila cukup query.
type costAccumulator interface {
	AccumulatedByProject(ctx context.Context, tenantID, projectID uint64) (domain.Money, error)
}

type eqParties struct {
	customers *customer.GORMRepository
	persons   *salesorg.GORMRepository
}

func (a *eqParties) CustomerExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.customers.FindByID(ctx, tenantID, id)
	return err
}
func (a *eqParties) SalesPersonExists(ctx context.Context, tenantID, id uint64) error {
	_, err := a.persons.FindPersonByID(ctx, tenantID, id)
	return err
}

// receiptAdapter: wiring kwitansi atomik (pola main.go).
type eqReceiptGen struct{ svc *billing.ReceiptService }

func (g *eqReceiptGen) GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64, amount domain.Money, bankAccountCode string, date time.Time, notes string) (string, uint64, error) {
	r, err := g.svc.GenerateReceiptInTx(ctx, tx, tenantID, createdBy, terminID, unitID, amount, bankAccountCode, date, notes)
	if err != nil {
		return "", 0, err
	}
	return r.ReceiptNumber, r.ID, nil
}

func eqSetup(t *testing.T) *eqEnv {
	t.Helper()
	db := eqConnect(t)
	eqCleanup(t, db)
	t.Cleanup(func() { eqCleanup(t, db) })
	ctx := context.Background()

	// Seed jalur produksi: COA penuh (dengan category cash/bank) + skema default.
	if err := ledger.SeedCOA(ctx, db, eqTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	if err := scheme.SeedDefaultSchemes(ctx, db, eqTenant); err != nil {
		t.Fatalf("seed schemes: %v", err)
	}
	// W-2: master jenis dokumen ikut jalur produksi (register → seed COA + jenis
	// dokumen). Tanpa ini kwitansi tidak bisa terbit sama sekali — resolver
	// penomoran fail-closed, persis seperti tenant baru yang belum di-seed.
	if err := document.SeedDefaultDocumentTypes(ctx, db, eqTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}

	// Wiring produksi (pola main.go / scheme_integration_test).
	ledgerRepo := ledger.NewGORMRepository(db)
	// Wiring PENUH seperti main.go: tanpa WithJournalTx, PostDraft/Post/Reverse
	// berjalan di luar transaksi — dan penolakan INV-DOC-1 justru meninggalkan
	// jurnal yang terlanjur posted. Fixture yang lebih longgar dari produksi
	// membuat suite ini membuktikan hal yang salah.
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).
		WithPeriodChecker(ledgerRepo).WithJournalTx(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)

	billingRepo := billing.NewGORMRepository(db)
	receiptSvc := billing.NewReceiptService(billingRepo, billingRepo, billingRepo)
	saleRepo.SetReceiptTxGenerator(&eqReceiptGen{svc: receiptSvc})

	custRepo := customer.NewGORMRepository(db)
	personRepo := salesorg.NewGORMRepository(db)
	svc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo), sale.WithPaymentCommitter(saleRepo),
		sale.WithBookingStore(saleRepo),
		sale.WithSchemeFlow(saleRepo, scheme.DefaultRegistry(), &eqParties{customers: custRepo, persons: personRepo}),
		// W-8: sale adalah PEMILIK sub-ledger piutang harga rumah; laporan dan
		// dashboard membacanya dari sini, bukan dari tabel jadwal.
		sale.WithHouseARStore(saleRepo),
		sale.WithHouseARLedger(ledger.NewLedgerBalanceService(ledger.NewQueryService(db))))
	// Product Catalog: resolver kebijakan produk dipasang seperti main.go —
	// suite ini menjalankan jalur produksi, bukan jalur legacy tanpa katalog.
	svc.SetProductPolicyResolver(project.NewGORMRepository(db))
	schemeSvc := scheme.NewService(scheme.NewGORMRepository(db), scheme.DefaultRegistry())

	budgetRepo := budget.NewGORMRepository(db)
	budgetSvc := budget.NewService(budgetRepo, budget.NewGORMRealisasiProvider(db))

	ledgerQ := ledger.NewQueryService(db)
	repRepo := NewGORMRepository(db)
	// S7: tax service produksi (pola tax.NewHandler) — reporting membaca
	// laporan pajak lewat pembaca kanonik ini.
	taxRepo := tax.NewGORMRepository(db, posting)
	taxSvc := tax.NewService(taxRepo, taxRepo, taxRepo, taxRepo,
		tax.WithBASTReader(taxRepo), tax.WithLedgerReader(taxRepo),
		tax.WithRuleResolution(taxRepo, taxRepo))
	if err := tax.SeedDefaultRates(ctx, db, eqTenant); err != nil {
		t.Fatalf("seed tax rates: %v", err)
	}
	repSvc := NewService(ledgerQ, repRepo, repRepo, repRepo).WithTaxReader(taxSvc).WithHouseReader(svc)

	env := &eqEnv{
		db: db, svc: svc, schemeSvc: schemeSvc, budgetSvc: budgetSvc, taxSvc: taxSvc,
		ledgerQ: ledgerQ, repSvc: repSvc, repRepo: repRepo, posting: posting,
		dash: &dashboardQuerier{db: db, finance: svc, repo: repRepo, budget: budgetSvc, house: svc}, kpr: &kprPipelineQuerier{db: db, finance: svc},
	}

	// ── Master data ──
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		eqTenant, "Konsistensi", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.projectID)

	if err := db.Exec(`INSERT INTO allocation_configs (tenant_id, project_id, basis) VALUES (?,?,?)`,
		eqTenant, env.projectID, "saleable_area").Error; err != nil {
		t.Fatalf("seed allocation config: %v", err)
	}

	// RAB active (raw — read-model budget yang diuji, bukan flow approval).
	if err := db.Exec(`INSERT INTO budget_plans (tenant_id, project_id, version, label, status) VALUES (?,?,?,?,?)`,
		eqTenant, env.projectID, 1, "RAB v1", "active").Error; err != nil {
		t.Fatalf("seed budget plan: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&env.planID)
	if err := db.Exec(`INSERT INTO budget_items (tenant_id, budget_plan_id, category, description, budgeted_amount) VALUES (?,?,?,?,?)`,
		eqTenant, env.planID, "construction", "Konstruksi", "600000000").Error; err != nil {
		t.Fatalf("seed budget item: %v", err)
	}

	cust, err := customer.NewService(custRepo).Create(ctx, eqTenant, customer.CreateCustomerRequest{Code: "EQ-C1", Name: "Budi"})
	if err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	env.customerID = cust.ID

	person := &salesorg.SalesPerson{TenantID: eqTenant, Code: "EQ-S1", Name: "Sari", IsActive: true}
	if err := personRepo.CreatePerson(ctx, person); err != nil {
		t.Fatalf("seed sales person: %v", err)
	}
	env.salesID = person.ID

	fs, err := schemeSvc.CreateFinancingSource(ctx, eqTenant, scheme.CreateFinancingSourceRequest{
		Code: "EQBTN", Name: "Bank BTN", Type: scheme.FinSourceKPRKomersial,
	})
	if err != nil {
		t.Fatalf("seed financing source: %v", err)
	}
	env.finSourceID = fs.ID

	schemes, err := schemeSvc.ListSchemes(ctx, eqTenant)
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

	env.unitA = env.seedUnit(t, "EQ-A1")
	env.unitB = env.seedUnit(t, "EQ-B1")
	return env
}

func (e *eqEnv) seedUnit(t *testing.T, code string) uint64 {
	t.Helper()
	// Product Catalog (fail-closed): unit_type WAJIB terdaftar di katalog.
	// Kategori 'property' + akun 4-1000 = perilaku fallback lama, jadi jurnal
	// yang dihasilkan fixture ini identik dengan sebelum hardening.
	e.db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, eqTenant, "rumah", "rumah", "property", "4-1000")
	if err := e.db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		eqTenant, e.projectID, code, "rumah", domain.FromInt(100), domain.FromInt(100), domain.FromInt(500_000_000), "available").Error; err != nil {
		t.Fatalf("seed unit %s: %v", code, err)
	}
	var id uint64
	e.db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

// postCost: biaya konstruksi aktual PROJECT-WIDE via PostingService produksi
// (Dr 1-3100 / Cr 2-1000, project-tagged) — basis actual-cost & HPP actual.
func (e *eqEnv) postCost(t *testing.T, amount int64, day time.Time) {
	t.Helper()
	ctx := context.Background()
	ids := e.accountIDs(t, "1-3100", "2-1000")
	pid := e.projectID
	entry, err := e.posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: eqTenant, Date: day, Description: "Biaya konstruksi (consistency)",
		Lines: []ledger.LineInput{
			{AccountID: ids["1-3100"], Debit: domain.FromInt(amount), ProjectID: &pid},
			{AccountID: ids["2-1000"], Credit: domain.FromInt(amount), ProjectID: &pid},
		},
	})
	if err != nil {
		t.Fatalf("cost journal: %v", err)
	}
	if _, err := e.posting.Post(ctx, eqTenant, entry.ID); err != nil {
		t.Fatalf("post cost journal: %v", err)
	}
}

// seedLegacyNotaryDeposit menanam satu titipan notaris WARISAN (pra-W-1): jurnal
// posted Dr 1-1300 / Cr 2-2300 + baris dokumen notary_deposits berstatus 'held'.
//
// Ditanam langsung, bukan lewat notary.ReceiveDeposit, karena jalur masuk itu
// sudah ditutup di W-1 (titipan baru wajib lewat Charge Group). Yang diuji di
// sini bukan cara membuatnya, melainkan bahwa saldo warisan tetap terekonsiliasi
// dan tetap bisa dibayarkan — data lama tidak boleh menjadi yatim.
func seedLegacyNotaryDeposit(t *testing.T, e *eqEnv, day time.Time, amount domain.Money) {
	t.Helper()
	ctx := context.Background()
	ids := e.accountIDs(t, "1-1300", "2-2300")
	unit := e.unitA
	entry, err := e.posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: eqTenant, Date: day, Description: "Titipan notaris diterima (warisan pra-W-1)",
		Lines: []ledger.LineInput{
			{AccountID: ids["1-1300"], Debit: amount, UnitID: &unit, Description: "Terima titipan notaris"},
			{AccountID: ids["2-2300"], Credit: amount, UnitID: &unit, Description: "Titipan Notaris (kewajiban)"},
		},
	})
	if err != nil {
		t.Fatalf("jurnal titipan notaris warisan: %v", err)
	}
	// Sejak W-3.5 kas tidak bisa bergerak tanpa bukti bernomor. Data warisan
	// seperti ini mendapat BKM lewat backfill W-3.3 (classifyCashJournal:
	// arah masuk → TypeCashIn), jadi fixture-nya menirukan hasil itu.
	if _, err := e.posting.PostDraft(ctx, eqTenant, entry.ID,
		ledger.DocumentSpec{TypeCode: ledger.DocCashIn}); err != nil {
		t.Fatalf("posting titipan notaris warisan: %v", err)
	}
	row := &notary.NotaryDeposit{
		TenantID: eqTenant, CustomerID: e.customerID, UnitID: &unit,
		Amount: amount, Status: notary.DepositHeld, NotaryName: "Notaris Konsistensi",
		ReceiveJournalID: entry.ID, ReceivedAt: day,
	}
	if err := e.db.WithContext(ctx).Create(row).Error; err != nil {
		t.Fatalf("simpan dokumen titipan warisan: %v", err)
	}
}

func (e *eqEnv) accountIDs(t *testing.T, codes ...string) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for _, c := range codes {
		var id uint64
		e.db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = ?`, eqTenant, c).Scan(&id)
		if id == 0 {
			t.Fatalf("akun %s tidak ada", c)
		}
		out[c] = id
	}
	return out
}

// tbBalanceByCodes: Σ Balance trial-balance (arah normal) untuk kode terpilih.
func (e *eqEnv) tbBalance(t *testing.T, tb *ledger.TrialBalance, pred func(code, category string) bool) string {
	t.Helper()
	cats := map[string]string{}
	accs, err := e.ledgerQ.ListAccounts(context.Background(), eqTenant)
	if err != nil {
		t.Fatalf("list accounts: %v", err)
	}
	for _, a := range accs {
		cats[a.Code] = string(a.Category)
	}
	sum := domain.Zero
	for _, r := range tb.Rows {
		if pred(r.AccountCode, cats[r.AccountCode]) {
			sum = sum.Add(r.Balance)
		}
	}
	return sum.String()
}

func (e *eqEnv) pay(t *testing.T, contractID uint64, amount int64, day time.Time, finSource *uint64) *sale.ReceivePaymentResult {
	t.Helper()
	src := sale.PaymentSourceCollection
	if finSource != nil {
		src = sale.PaymentSourceKPRDisbursement
	}
	cid := contractID
	res, err := e.svc.ReceivePayment(context.Background(), eqTenant, sale.ReceivePaymentRequest{
		Source: src, ContractID: &cid, Amount: domain.FromInt(amount), Date: day,
		BankAccountCode: "1-1300", FinancingSourceID: finSource,
	})
	if err != nil {
		t.Fatalf("ReceivePayment %d: %v", amount, err)
	}
	return res
}

func (e *eqEnv) event(t *testing.T, contractID uint64, ev scheme.Event) {
	t.Helper()
	fin := e.finSourceID
	if _, err := e.svc.ApplySchemeEvent(context.Background(), eqTenant, sale.ApplySchemeEventRequest{
		ContractID: contractID, Event: ev, FinancingSourceID: &fin,
	}); err != nil {
		t.Fatalf("ApplySchemeEvent %s: %v", ev, err)
	}
}

// ═════════════════════════════════════════════════════════════════════════════
// SUITE
// ═════════════════════════════════════════════════════════════════════════════

func TestConsistency_FinancialNumbers(t *testing.T) {
	env := eqSetup(t)
	ctx := context.Background()
	day := time.Now().Truncate(24 * time.Hour).UTC()

	// ── Skenario ─────────────────────────────────────────────────────────────
	// 1. Biaya konstruksi aktual 300jt (Dr 1-3100 / Cr 2-1000).
	env.postCost(t, 300_000_000, day)

	// 2. Booking unit A fee 5jt → konversi ke kontrak KPR 500jt.
	// Refundable:false (RULE KLIEN 2026-07-29, default): fee diakui langsung
	// sebagai Pendapatan Booking, tidak pernah menyentuh 2-2100 — skenario ini
	// tidak pernah men-dispose/forfeit fee A, jadi jalur legacy held tidak
	// relevan di sini (lihat assersi EQ_BookingLiability/EQ_R4_BookingFee).
	bkA, err := env.svc.CreateBooking(ctx, eqTenant, sale.CreateBookingRequest{
		UnitID: env.unitA, CustomerID: env.customerID, SalesPersonID: &env.salesID,
		BookingFee: domain.FromInt(5_000_000), Refundable: false,
		BankAccountCode: "1-1300", BookingDate: day, ExpiryDate: day.AddDate(0, 1, 0),
	})
	if err != nil {
		t.Fatalf("booking A: %v", err)
	}
	cA, err := env.svc.CreateContract(ctx, eqTenant, sale.CreateContractRequest{
		UnitID: env.unitA, BuyerName: "Budi", BuyerID: "3201", ContractDate: day,
		TotalPrice:      domain.FromInt(500_000_000),
		PaymentSchemeID: &env.schemeKPRID, FinancingSourceID: &env.finSourceID,
		CustomerID: &env.customerID, SalesPersonID: &env.salesID, BookingID: &bkA.ID,
	})
	if err != nil {
		t.Fatalf("kontrak A: %v", err)
	}
	env.contractAID = cA.ID

	// 3. Jadwal 2 baris (dp 100jt +20 hari, final 400jt +80 hari) → basis forecast.
	if _, err := env.svc.CreatePaymentSchedule(ctx, eqTenant, cA.ID, []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: day.AddDate(0, 0, 20), Amount: domain.FromInt(100_000_000), Type: sale.ScheduleTypeDP},
		{InstallmentNumber: 2, DueDate: day.AddDate(0, 0, 80), Amount: domain.FromInt(400_000_000), Type: sale.ScheduleTypeFinal},
	}); err != nil {
		t.Fatalf("jadwal: %v", err)
	}

	// 4. DP 45jt → milestone KPR sampai akad → pencairan 430jt.
	env.pay(t, cA.ID, 45_000_000, day, nil)
	env.event(t, cA.ID, scheme.EventSubmittedToBank)
	env.event(t, cA.ID, scheme.EventBankApproved)
	env.event(t, cA.ID, scheme.EventAkad)
	env.pay(t, cA.ID, 430_000_000, day, &env.finSourceID)
	// Posisi kontrak A: dibayar 5+45+430 = 480jt; outstanding 20jt.

	// 5. Booking unit B fee 10jt → cancel. RULE KLIEN 2026-07-29: fee sudah
	//    diakui Pendapatan Booking (4-2100) saat diterima — batal TANPA
	//    jurnal/reversal (tidak ada forfeit ke 4-2000 lagi).
	bkB, err := env.svc.CreateBooking(ctx, eqTenant, sale.CreateBookingRequest{
		UnitID: env.unitB, CustomerID: env.customerID,
		BookingFee: domain.FromInt(10_000_000), Refundable: false,
		BankAccountCode: "1-1300", BookingDate: day, ExpiryDate: day.AddDate(0, 1, 0),
	})
	if err != nil {
		t.Fatalf("booking B: %v", err)
	}
	if _, err := env.svc.CancelBooking(ctx, eqTenant, bkB.ID, "batal (consistency)", day, nil); err != nil {
		t.Fatalf("cancel booking B: %v", err)
	}

	// 6. BAST unit A (HPP actual: 300jt project-wide ÷ 2 unit ber-area sama = 150jt).
	if _, err := env.svc.RecordAkad(ctx, eqTenant, sale.RecordBASTRequest{
		UnitID: env.unitA, SalePrice: domain.FromInt(500_000_000),
		BuyerRef: "Budi", BASTDate: day,
	}); err != nil {
		t.Fatalf("BAST A: %v", err)
	}

	// 7. Akrual PPh Final pengalihan unit A (Event 5a, jalur produksi tax) —
	//    basis EQ_TaxLiability (S7): Dr Beban PPh / Cr Hutang PPh 2-4000.
	if _, err := env.taxSvc.AccrueTax(ctx, eqTenant, tax.AccrueTaxRequest{
		RateCode:      tax.RateCodePPhFinalPengalihan,
		TransferValue: domain.FromInt(500_000_000),
		AccrualDate:   day, UnitID: &env.unitA, ProjectID: &env.projectID,
	}); err != nil {
		t.Fatalf("AccrueTax: %v", err)
	}

	// 8. Titipan biaya realisasi — DUA generasi hidup berdampingan:
	//
	//  8a. WARISAN (pra-W-1): titipan notaris 3jt di 2-2300 lewat modul
	//      internal/notary. Jalur masuknya sudah ditutup, jadi datanya ditanam
	//      langsung — persis bentuk yang ada di DB produksi hari ini. Basis
	//      EQ_NotaryDeposit; saldonya hanya boleh menyusut lewat payout.
	seedLegacyNotaryDeposit(t, env, day, domain.FromInt(3_000_000))

	//  8b. SEKARANG (W-1): biaya notaris masuk sebagai item Charge Group dan
	//      mendarat di 2-2400. Rekonsiliasinya (EQ_RealizationDeposit) hidup di
	//      internal/charge — paket charge meng-import reporting, jadi suite ini
	//      tidak boleh meng-import charge (siklus). Lihat
	//      TestIntegration_EQ_RealizationDeposit di internal/charge.

	// ── Snapshot pembacaan ────────────────────────────────────────────────────
	asOf := day.AddDate(0, 0, 1) // inklusif seluruh transaksi hari ini
	dash, err := env.dash.GetDashboard(ctx, eqTenant, time.Now())
	if err != nil {
		t.Fatalf("dashboard: %v", err)
	}
	tb, err := env.ledgerQ.TrialBalance(ctx, eqTenant, asOf)
	if err != nil {
		t.Fatalf("trial balance: %v", err)
	}
	sum, err := env.svc.ContractFinancialSummaryByID(ctx, eqTenant, cA.ID)
	if err != nil {
		t.Fatalf("summary: %v", err)
	}
	var dp *DashboardProject
	for i := range dash.Projects {
		if dash.Projects[i].ID == env.projectID {
			dp = &dash.Projects[i]
		}
	}
	if dp == nil {
		t.Fatal("proyek tidak ada di dashboard")
	}

	// ═════ #1/#2 CASH & BANK — dashboard vs trial balance ═════
	t.Run("EQ_Cash_Dashboard_vs_TrialBalance", func(t *testing.T) {
		want := env.tbBalance(t, tb, func(_, cat string) bool { return cat == "cash" || cat == "bank" })
		moneyEq(t, "cash: dashboard KPI vs Σ trial-balance(category cash|bank)", dash.KPI.Cash, want)
	})

	// ═════ #3 AR (ledger) ═════
	t.Run("EQ_AR_Dashboard_vs_TrialBalance", func(t *testing.T) {
		want := env.tbBalance(t, tb, func(code, _ string) bool {
			return code == "1-2000" || code == "1-2100" || code == "1-2200"
		})
		moneyEq(t, "receivable: dashboard KPI vs trial-balance 1-2xxx", dash.KPI.Receivable, want)
	})

	// ═════ #4 AP ═════
	t.Run("EQ_AP_Dashboard_vs_TrialBalance", func(t *testing.T) {
		want := env.tbBalance(t, tb, func(code, _ string) bool { return code == "2-1000" })
		moneyEq(t, "payable: dashboard KPI vs trial-balance 2-1000", dash.KPI.Payable, want)
	})

	// ═════ #5 BOOKING — rule klien 2026-07-29: fee = PENDAPATAN, bukan liability ═════
	t.Run("EQ_BookingLiability_Ledger_vs_Dokumen", func(t *testing.T) {
		// 2-2100 kini murni LEGACY: booking baru tidak pernah menyentuhnya —
		// skenario ini membuktikannya (saldo 0 == Σ fee held 0).
		want := env.tbBalance(t, tb, func(code, _ string) bool { return code == "2-2100" })
		moneyEq(t, "titipan legacy: dashboard KPI vs trial-balance 2-2100", dash.KPI.BookingDeposits, want)
		moneyEq(t, "titipan legacy: Σ fee held (dokumen) vs saldo ledger 2-2100", dash.KPI.BookingFeeHeld, want)
		moneyEq(t, "titipan legacy: booking baru tidak menyentuh 2-2100", want, "0")
	})
	t.Run("EQ_BookingRevenue_Ledger_vs_Dokumen", func(t *testing.T) {
		// Pendapatan Booking (4-2100) == Σ fee booking ber-disposisi
		// 'recognized' (dokumen) — pendapatan final, batal/konversi tidak
		// mengubahnya (A dikonversi + B dibatalkan → keduanya tetap dihitung).
		want := env.tbBalance(t, tb, func(code, _ string) bool { return code == "4-2100" })
		var docFee string
		env.db.Raw(`SELECT COALESCE(SUM(booking_fee),0) FROM bookings
			WHERE tenant_id = ? AND fee_disposition = 'recognized'`, eqTenant).Scan(&docFee)
		moneyEq(t, "pendapatan booking: ledger 4-2100 vs Σ fee recognized", want, docFee)
		moneyEq(t, "pendapatan booking: A(5jt)+B(10jt) tetap diakui", want, domain.FromInt(15_000_000).String())
	})

	// ═════ #16 COMMISSION payable (ledger; skenario tanpa komisi → 0 == 0) ═════
	t.Run("EQ_CommissionPayable_Dashboard_vs_TrialBalance", func(t *testing.T) {
		want := env.tbBalance(t, tb, func(code, _ string) bool { return code == "2-6200" })
		moneyEq(t, "utang komisi: dashboard KPI vs trial-balance 2-6200", dash.KPI.CommissionPayable, want)
	})

	// ═════ #7 OUTSTANDING — kanonik vs semua pembaca ═════
	t.Run("EQ_Outstanding_Dashboard_vs_SummarySum", func(t *testing.T) {
		if dash.Receivables == nil {
			t.Fatal("dashboard.receivables kosong")
		}
		// Skenario: satu kontrak aktif → Σ = summary kontrak A.
		moneyEq(t, "outstanding: dashboard receivables vs Σ ContractFinancialSummary",
			dash.Receivables.OutstandingTotal, sum.Outstanding.String())
	})
	t.Run("EQ_Outstanding_KPR_vs_Summary", func(t *testing.T) {
		rep, err := env.kpr.GetKPRPipeline(ctx, eqTenant, time.Now())
		if err != nil {
			t.Fatalf("kpr pipeline: %v", err)
		}
		var found bool
		for _, r := range rep.Rows {
			if r.ContractID == env.contractAID {
				found = true
				moneyEq(t, "outstanding: KPR pipeline row vs summary", r.Outstanding, sum.Outstanding.String())
				moneyEq(t, "total_paid: KPR pipeline row vs summary", r.TotalPaid, sum.TotalPaid.String())
			}
		}
		if !found {
			t.Fatal("kontrak A tidak ada di KPR pipeline")
		}
		moneyEq(t, "outstanding total: KPR pipeline vs summary", rep.TotalOutstanding, sum.Outstanding.String())
	})
	t.Run("EQ_Outstanding_Statement_vs_Summary", func(t *testing.T) {
		st, err := env.svc.GetCustomerStatement(ctx, eqTenant, env.contractAID, asOf)
		if err != nil {
			t.Fatalf("statement: %v", err)
		}
		moneyEq(t, "outstanding: statement vs summary", st.RemainingBalance, sum.Outstanding.String())
		moneyEq(t, "total_paid: statement vs summary", st.TotalPaid, sum.TotalPaid.String())
	})
	t.Run("EQ_Outstanding_CollectionPreview_vs_Summary", func(t *testing.T) {
		pv, err := env.svc.PreviewCollectionPayment(ctx, eqTenant, env.contractAID, domain.FromInt(1_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection)
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		moneyEq(t, "outstanding: collection preview vs summary", pv.Outstanding, sum.Outstanding.String())
	})

	// ═════ T-3 — KEKURANGAN PASCA-PENCAIRAN = PIUTANG CUSTOMER ═════
	// Keputusan klien final 2026-08-05. Skenario di atas persis contoh klien:
	// harga 500jt, diterima customer 50jt (5jt booking + 45jt DP), bank cair
	// 430jt → sisa 20jt. Bank SELESAI pada nilai pencairan aktualnya, jadi
	// seluruh sisa harus berada di Piutang Customer — nol di Piutang Bank.
	t.Run("EQ_T3_Outstanding_BukanPiutangBank", func(t *testing.T) {
		bank := env.tbBalance(t, tb, func(code, _ string) bool { return code == "1-2200" })
		moneyEq(t, "saldo Piutang Bank 1-2200 pasca-pencairan", bank, "0")

		customer := env.tbBalance(t, tb, func(code, _ string) bool { return code == "1-2000" })
		moneyEq(t, "Piutang Customer 1-2000 vs outstanding kanonik", customer, sum.Outstanding.String())
	})
	t.Run("EQ_T3_CreditAccount_Preview_vs_Posting", func(t *testing.T) {
		pv, err := env.svc.PreviewCollectionPayment(ctx, eqTenant, env.contractAID, domain.FromInt(1_000_000), "1-1300", domain.Zero, 0, sale.PaymentSourceCollection)
		if err != nil {
			t.Fatalf("preview: %v", err)
		}
		if len(pv.Lines) != 2 {
			t.Fatalf("pratinjau harus 2 baris, got %d", len(pv.Lines))
		}
		// Pratinjau yang dilihat kasir HARUS akun yang sama dengan jurnal yang
		// benar-benar terbentuk — pelunasan kekurangan mengkredit customer.
		if pv.Lines[1].AccountCode != "1-2000" {
			t.Errorf("akun kredit pratinjau = %s, want 1-2000 (piutang customer)", pv.Lines[1].AccountCode)
		}
	})

	// ═════ #8 TOTAL PAID — statement vs kwitansi vs ledger kas ═════
	// R4: kwitansi booking fee TETAP terbit (bukti penerimaan sah), tapi fee
	// outside-price TIDAK termasuk TotalPaid — pembanding difilter flag kanonik.
	t.Run("EQ_TotalPaid_vs_Receipts", func(t *testing.T) {
		var receiptsA string
		env.db.Raw(`SELECT COALESCE(SUM(r.amount),0) FROM receipts r
			JOIN termin_payments tp ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
			WHERE r.tenant_id = ? AND tp.unit_id = ? AND tp.counts_toward_price = TRUE`,
			eqTenant, env.unitA).Scan(&receiptsA)
		moneyEq(t, "total_paid: summary vs Σ kwitansi unit A (counts_toward_price)", sum.TotalPaid.String(), receiptsA)
	})

	// ═════ R4 — booking fee di LUAR harga (keputusan PO #3) ═════
	t.Run("EQ_R4_BookingFee_OutsidePrice", func(t *testing.T) {
		// Outstanding kontrak A = gross penuh − pembayaran HARGA (45+430),
		// fee 5jt TIDAK mengurangi → 25jt persis.
		moneyEq(t, "outstanding: fee tidak mengurangi harga",
			sum.Outstanding.String(), domain.FromInt(25_000_000).String())
		moneyEq(t, "total_paid: hanya pembayaran harga",
			sum.TotalPaid.String(), domain.FromInt(475_000_000).String())

		// Rule klien: fee TETAP Pendapatan Booking pasca-konversi — disposisi
		// 'recognized' (final; tidak pernah direklas/menjadi bagian harga).
		var disp string
		env.db.Raw(`SELECT fee_disposition FROM bookings WHERE tenant_id = ? AND unit_id = ? AND status = 'converted'`,
			eqTenant, env.unitA).Scan(&disp)
		if disp != "recognized" {
			t.Errorf("fee_disposition booking converted = %q, want recognized (pendapatan final)", disp)
		}
		// Tidak ada buyer_credit dari fee outside-price.
		var bc string
		env.db.Raw(`SELECT COALESCE(SUM(pa.amount),0) FROM payment_allocations pa
			JOIN termin_payments tp ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id
			WHERE pa.tenant_id = ? AND pa.allocation_type = 'buyer_credit'
			  AND tp.payment_source = 'booking_fee'`, eqTenant).Scan(&bc)
		moneyEq(t, "buyer_credit dari fee outside-price harus nol", bc, "0")

		// Dashboard collected proyek == Σ TotalPaid kanonik (satu rumus R4).
		moneyEq(t, "collected proyek vs Σ TotalPaid kanonik", dp.Collected, sum.TotalPaid.String())
		moneyEq(t, "contract_value proyek vs NetContract", dp.ContractValue, sum.NetContract.String())
	})
	t.Run("EQ_Payments_vs_LedgerCashIn", func(t *testing.T) {
		// Skenario terkontrol: kas masuk = Σ termin + Σ titipan notaris
		// (dua-duanya sub-ledger dokumen ber-jurnal — rekonsiliasi penuh).
		var termins, notaryIn string
		env.db.Raw(`SELECT COALESCE(SUM(amount),0) FROM termin_payments WHERE tenant_id = ?`, eqTenant).Scan(&termins)
		env.db.Raw(`SELECT COALESCE(SUM(amount),0) FROM notary_deposits WHERE tenant_id = ?`, eqTenant).Scan(&notaryIn)
		var cashIn string
		env.db.Raw(`SELECT COALESCE(SUM(jl.debit),0)
			FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.journal_entry_id
			JOIN accounts a ON a.id = jl.account_id
			WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.category IN ('cash','bank')`,
			eqTenant).Scan(&cashIn)
		tm, _ := domain.NewMoney(termins)
		nt, _ := domain.NewMoney(notaryIn)
		moneyEq(t, "Σ termin + Σ titipan notaris vs Σ debit kas/bank (ledger)", tm.Add(nt).String(), cashIn)
	})

	// ═════ UAT Batch 2 §3 — Titipan Notaris WARISAN: LIABILITY, bukan pendapatan ═════
	t.Run("EQ_NotaryDeposit_Ledger_vs_Dokumen", func(t *testing.T) {
		want := env.tbBalance(t, tb, func(code, _ string) bool { return code == "2-2300" })
		var docHeld string
		env.db.Raw(`SELECT COALESCE(SUM(amount),0) FROM notary_deposits
			WHERE tenant_id = ? AND status = 'held'`, eqTenant).Scan(&docHeld)
		moneyEq(t, "titipan notaris: saldo ledger 2-2300 vs Σ deposit held", want, docHeld)
		moneyEq(t, "titipan notaris: 3jt warisan tertahan", want, domain.FromInt(3_000_000).String())
		// Tidak pernah menyentuh pendapatan: 4-% TIDAK berubah karena notaris —
		// dijamin EQ_Revenue (dashboard==P&L) + baris jurnal notaris hanya
		// kas↔2-2300 (diverifikasi rekonsiliasi kas di atas).
	})

	// ═════ UAT Batch 2 §1 — Kwitansi booking TERPISAH (KWB) ═════
	t.Run("DOC_BookingReceipt_Separate", func(t *testing.T) {
		var rec struct {
			Rt string `gorm:"column:receipt_type"`
			Rn string `gorm:"column:receipt_number"`
		}
		env.db.Raw(`SELECT r.receipt_type, r.receipt_number FROM receipts r
			JOIN termin_payments tp ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
			WHERE r.tenant_id = ? AND tp.payment_source = 'booking_fee' LIMIT 1`, eqTenant).Scan(&rec)
		rt, rn := rec.Rt, rec.Rn
		if rt != "booking" || len(rn) < 4 || rn[:4] != "KWB/" {
			t.Errorf("kwitansi booking = %s/%s, want type=booking + nomor KWB/", rt, rn)
		}
		// Setiap sumber pembayaran punya SERI kwitansinya sendiri — buku kwitansi
		// customer (KWT) tidak boleh memuat lembar yang pembayarnya bukan customer
		// (pencairan bank → KWD) atau yang bukan harga unit (booking → KWB,
		// biaya realisasi → KWR). Dicek menyeluruh lewat pemetaan, bukan hanya
		// "selain booking pasti KWT".
		var mismatched int64
		env.db.Raw(`SELECT COUNT(*) FROM receipts r
			JOIN termin_payments tp ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
			WHERE r.tenant_id = ?
			  AND (r.receipt_type, LEFT(r.receipt_number, 4)) <> (CASE tp.payment_source
			        WHEN 'booking_fee'          THEN ROW('booking',          'KWB/')
			        WHEN 'kpr_disbursement'     THEN ROW('kpr_disbursement', 'KWD/')
			        WHEN 'realization'          THEN ROW('realization',      'KWR/')
			        WHEN 'realization_transfer' THEN ROW('realization',      'KWR/')
			        ELSE                             ROW('house_payment',    'KWT/') END)`,
			eqTenant).Scan(&mismatched)
		if mismatched != 0 {
			var detail []struct {
				Src string `gorm:"column:payment_source"`
				Rt  string `gorm:"column:receipt_type"`
				Rn  string `gorm:"column:receipt_number"`
			}
			env.db.Raw(`SELECT tp.payment_source, r.receipt_type, r.receipt_number FROM receipts r
				JOIN termin_payments tp ON tp.id = r.termin_payment_id AND tp.tenant_id = r.tenant_id
				WHERE r.tenant_id = ?`, eqTenant).Scan(&detail)
			t.Errorf("%d kwitansi memakai seri di luar pemetaan sumber→seri; isi buku: %+v", mismatched, detail)
		}
	})

	// ═════ #9/#11 REVENUE & MARGIN — dashboard vs Laba Rugi kanonik ═════
	pl, err := env.repSvc.GetProjectPL(ctx, eqTenant, env.projectID, nil, asOf)
	if err != nil {
		t.Fatalf("project PL: %v", err)
	}
	// S4 DONE (keputusan PO #1): dashboard revenue/margin = ComputePL —
	// kesetaraan ditegakkan PERMANEN (knownDivergence F5 dihapus).
	t.Run("EQ_Revenue_DashboardProject_vs_PL", func(t *testing.T) {
		moneyEq(t, "revenue: dashboard proyek vs P&L pendapatan", dp.Revenue, pl.TotalPendapatan)
	})
	t.Run("EQ_Margin_DashboardProject_vs_PL", func(t *testing.T) {
		moneyEq(t, "margin: dashboard proyek vs P&L laba", dp.Margin, pl.LabaRugiBersih)
	})
	t.Run("EQ_RevenueMTD_Dashboard_vs_PL", func(t *testing.T) {
		// KPI MTD memakai SEMUA 4-% (sama dgn P&L) — seluruh transaksi bulan ini.
		moneyEq(t, "sales MTD: dashboard KPI vs P&L pendapatan", dash.KPI.SalesMTD, pl.TotalPendapatan)
	})

	// ═════ #10 HPP — ledger vs snapshot sale_records vs unit_profits ═════
	t.Run("EQ_HPP_Ledger_vs_SaleRecords", func(t *testing.T) {
		var ledgerHPP string
		env.db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit),0)
			FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.journal_entry_id
			JOIN accounts a ON a.id = jl.account_id
			WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = '5-1000'`, eqTenant).Scan(&ledgerHPP)
		var srHPP string
		env.db.Raw(`SELECT COALESCE(SUM(hpp_land + hpp_hard + hpp_soft + hpp_financing),0)
			FROM sale_records WHERE tenant_id = ? AND cancelled_at IS NULL`, eqTenant).Scan(&srHPP)
		moneyEq(t, "HPP: ledger 5-1000 vs Σ sale_records.hpp_*", ledgerHPP, srHPP)
		moneyEq(t, "HPP: dashboard proyek vs ledger 5-1000", dp.HPP, ledgerHPP)
		if len(dash.UnitProfits) == 0 {
			t.Fatal("unit_profits kosong")
		}
		moneyEq(t, "HPP: unit_profits vs ledger 5-1000", dash.UnitProfits[0].HPP, ledgerHPP)
	})

	// ═════ #12 BUDGET — dashboard vs modul budget ═════
	t.Run("EQ_Budget_Dashboard_vs_BudgetService", func(t *testing.T) {
		rep, err := env.budgetSvc.GetRABvsRealisasi(ctx, eqTenant, env.projectID, nil)
		if err != nil {
			t.Fatalf("rab-vs-realisasi: %v", err)
		}
		moneyEq(t, "budget: dashboard budget_total vs budget.TotalBudgeted", dp.BudgetTotal, rep.TotalBudgeted)
	})

	// ═════ #13 ACTUAL COST — S5 DONE (keputusan PO #2): SATU definisi ═════
	// Kanonik = ledger, netting debit − kredit-reversal (relief BAST TIDAK
	// mengurangi). Dashboard == cost repo == budget realisasi — permanen.
	t.Run("EQ_ActualCost_Dashboard_vs_CostRepo", func(t *testing.T) {
		costRepo := cost.NewGORMRepository(env.db)
		bd, err := costRepo.AccumulatedByProject(ctx, eqTenant, env.projectID)
		if err != nil {
			t.Fatalf("AccumulatedByProject: %v", err)
		}
		moneyEq(t, "actual cost: dashboard vs cost.AccumulatedByProject (kanonik)",
			dp.ActualCost, bd.Total().String())

		// Netting kanonik menahan relief BAST: skenario memposting 300jt dan
		// BAST mengkredit 150jt (source sale, BUKAN reversal) → actual tetap 300jt.
		moneyEq(t, "actual cost: relief BAST tidak mengurangi (PO #2)",
			dp.ActualCost, domain.FromInt(300_000_000).String())

		// Budget realisasi (RAB vs Realisasi) membaca sumber yang SAMA.
		rr, err := env.budgetSvc.GetRABvsRealisasi(ctx, eqTenant, env.projectID, nil)
		if err != nil {
			t.Fatalf("GetRABvsRealisasi: %v", err)
		}
		// Skenario tanpa biaya expense (marketing/other) → total realisasi ==
		// kapitalisasi == dashboard actual_cost.
		moneyEq(t, "actual cost: budget TotalRealisasi vs dashboard", rr.TotalRealisasi, dp.ActualCost)
	})

	// ═════ Rule #6 (UAT 2026-09-03) PERSEDIAAN — Neraca vs kapitalisasi dikurangi HPP relieved ═════
	// Persediaan (akun 1-3xxx, empat sub-akun kategori biaya) harus == kapitalisasi
	// kanonik (dp.ActualCost, S5 — TIDAK berkurang oleh relief BAST) dikurangi HPP
	// yang sudah direlief ke L/R (ledger 5-1000). Ini membuktikan Neraca/persediaan
	// konsisten dengan L/R dan jurnal transaksi, bukan kalkulasi kedua yang terpisah.
	t.Run("EQ_Persediaan_Neraca_vs_CapitalizedMinusHPP", func(t *testing.T) {
		persediaan := env.tbBalance(t, tb, func(code, _ string) bool {
			return code == "1-3000" || code == "1-3100" || code == "1-3200" || code == "1-3300"
		})
		var ledgerHPPRaw string
		env.db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit),0)
			FROM journal_lines jl
			JOIN journal_entries je ON je.id = jl.journal_entry_id
			JOIN accounts a ON a.id = jl.account_id
			WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = '5-1000'`, eqTenant).Scan(&ledgerHPPRaw)

		actual, err := domain.NewMoney(dp.ActualCost)
		if err != nil {
			t.Fatalf("dp.ActualCost bukan money: %q", dp.ActualCost)
		}
		ledgerHPP, err := domain.NewMoney(ledgerHPPRaw)
		if err != nil {
			t.Fatalf("ledgerHPP bukan money: %q", ledgerHPPRaw)
		}
		want := actual.Sub(ledgerHPP).String()
		moneyEq(t, "persediaan: Neraca Σ1-3xxx vs kapitalisasi(S5) − HPP relieved(5-1000)", persediaan, want)

		// Skenario: kapitalisasi 300jt (postCost), BAST unit A relief 150jt →
		// sisa 150jt tetap sebagai persediaan (unit B di proyek yg sama belum BAST).
		moneyEq(t, "persediaan: sisa 150jt (unit B proyek sama belum BAST)", persediaan, domain.FromInt(150_000_000).String())

		// Per sub-akun: skenario ini hanya memposting biaya konstruksi (1-3100) —
		// tanah/perizinan/pembiayaan (1-3000/1-3200/1-3300) harus nol, dan seluruh
		// saldo persediaan berasal dari 1-3100 saja.
		for _, code := range []string{"1-3000", "1-3200", "1-3300"} {
			zero := env.tbBalance(t, tb, func(c, _ string) bool { return c == code })
			moneyEq(t, "persediaan sub-akun "+code+" harus nol (tanpa biaya kategori ini)", zero, "0")
		}
		hard := env.tbBalance(t, tb, func(c, _ string) bool { return c == "1-3100" })
		moneyEq(t, "persediaan 1-3100 (hard/konstruksi) vs total Σ1-3xxx (satu-satunya kategori aktif)", hard, persediaan)
	})

	// ═════ #19 FORECAST CASH — dashboard vs BuildARAging ═════
	t.Run("EQ_Forecast_Dashboard_vs_ARAging", func(t *testing.T) {
		if dash.Receivables == nil {
			t.Fatal("dashboard.receivables kosong")
		}
		// W-8: sumber baris piutang adalah pemilik sub-ledgernya (sale.Service),
		// sumber yang SAMA dengan yang dipakai dashboard — kalau keduanya
		// dibiarkan membaca tabel yang berbeda, kesetaraan di bawah hanya
		// membuktikan bahwa dua query salah dengan cara yang sama.
		rows, err := env.svc.ReceivableRows(ctx, eqTenant)
		if err != nil {
			t.Fatalf("ar rows: %v", err)
		}
		ar := BuildARAging(rows, time.Now())
		moneyEq(t, "forecast 30: dashboard vs ARAging", dash.Receivables.Forecast30, ar.Expected30)
		moneyEq(t, "forecast 60: dashboard vs ARAging", dash.Receivables.Forecast60, ar.Expected60)
		moneyEq(t, "forecast 90: dashboard vs ARAging", dash.Receivables.Forecast90, ar.Expected90)
	})

	// ═════ #14 PPh — S7 DONE: reporting == tax kanonik == ledger 2-4000 ═════
	t.Run("EQ_TaxLiability_Report_vs_Ledger", func(t *testing.T) {
		from := day.AddDate(0, 0, -1)
		rpt, err := env.repSvc.GetTaxLiability(ctx, eqTenant, from, asOf)
		if err != nil {
			t.Fatalf("GetTaxLiability: %v", err)
		}
		taxRpt, err := env.taxSvc.GetTaxReport(ctx, eqTenant, from, asOf)
		if err != nil {
			t.Fatalf("GetTaxReport: %v", err)
		}
		// Reporting == pembaca kanonik tax (satu pembaca, R-7).
		moneyEq(t, "pajak: reporting total vs tax kanonik", rpt.TotalObligation, taxRpt.TotalObligation)
		moneyEq(t, "pajak: reporting outstanding vs tax kanonik", rpt.TotalOutstanding, taxRpt.TotalOutstanding)
		if len(rpt.Items) != len(taxRpt.Items) {
			t.Errorf("jumlah item: reporting %d vs tax %d", len(rpt.Items), len(taxRpt.Items))
		}
		// Rekonsiliasi obligations ↔ ledger 2-4000 (registry #14).
		want := env.tbBalance(t, tb, func(code, _ string) bool { return code == "2-4000" })
		moneyEq(t, "pajak: Σ obligations outstanding vs saldo ledger 2-4000", rpt.TotalOutstanding, want)
		moneyEq(t, "pajak: ledger_outstanding payload vs trial balance", rpt.LedgerOutstanding, want)
		if !rpt.Reconciled {
			t.Error("laporan pajak harus reconciled (obligations == ledger 2-4000)")
		}
	})

	// ═════ #6 BUYER CREDIT — kanonik vs sumber konversi ═════
	t.Run("EQ_BuyerCredit_Canonical", func(t *testing.T) {
		var alloc, applied string
		env.db.Raw(`SELECT COALESCE(SUM(pa.amount),0) FROM payment_allocations pa
			JOIN termin_payments tp ON tp.id = pa.termin_payment_id AND tp.tenant_id = pa.tenant_id
			WHERE pa.tenant_id = ? AND pa.allocation_type = 'buyer_credit' AND tp.unit_id = ?`,
			eqTenant, env.unitA).Scan(&alloc)
		env.db.Raw(`SELECT COALESCE(SUM(ca.amount),0) FROM credit_applications ca
			WHERE ca.tenant_id = ? AND ca.unit_id = ?`, eqTenant, env.unitA).Scan(&applied)
		a, _ := domain.NewMoney(alloc)
		b, _ := domain.NewMoney(applied)
		_ = a.Sub(b) // saldo kanonik — dipakai bila endpoint dibandingkan di v2
		// v1: pastikan sumber tunggal konsisten dengan dirinya (alloc >= applied).
		if a.Decimal().LessThan(b.Decimal()) {
			t.Errorf("buyer credit: applied (%s) > sumber (%s)", b, a)
		}
	})
}
