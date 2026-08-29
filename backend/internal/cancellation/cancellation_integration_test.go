//go:build integration

package cancellation_test

// Increment 8 — integration (real MySQL). Tiga siklus:
//  (1) PRA-BAST : termin 300jt → cancel penalti 50jt → settlement
//                 (Dr 2-2000 300 / Cr 4-2000 50 / Cr 2-2200 250), unit release,
//                 refund pending 250jt → dibayar (Dr 2-2200 / Cr Bank).
//  (2) PASCA-BAST (INV-COGS-SUM PENUH, termasuk porsi TRUE-UP): skenario P0-4
//                 (RAB 400jt, aktual 480jt, BAST budgeted 100jt, true-up +20jt
//                 posted) → cancel → reverse Event 3 + mirror COGS (Event 4 +
//                 true-up unit) + reverse PPh + settlement; 5-1000 unit = 0,
//                 1-3000 kembali 480jt, sale_record ditandai, unit available.
//  (3) BOOKING  : fee refundable pending_refund → refund → paid → disposisi
//                 'refunded', 2-2100 = 0.
// Prasyarat: TEST_DB_DSN + migrasi ≥ 000045.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/allocation"
	"esaproperti/internal/billing"
	"esaproperti/internal/budget"
	"esaproperti/internal/cancellation"
	"esaproperti/internal/closing"
	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
	"esaproperti/internal/tax"
)

const cxTenant uint64 = 9_900_009

func cxConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func cxCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	// land_sales <-> land_stock_reservations adalah FK sirkular
	// (reservation.converted_sale_id -> land_sales, land_sales.reservation_id
	// -> reservation) — putus siklusnya dulu (no-op untuk test tanpa komponen
	// tanah). Lihat lbCleanup di internal/sale/land_integration_test.go.
	if err := db.Exec(`UPDATE land_stock_reservations SET converted_sale_id = NULL WHERE tenant_id = ?`, cxTenant).Error; err != nil {
		t.Fatalf("cleanup putus siklus land_stock_reservations: %v", err)
	}
	for _, tbl := range []string{
		"refunds", "cancellations",
		"hpp_trueup_lines", "hpp_trueup_runs", "project_completion_events",
		"allocation_snapshot_lines", "allocation_snapshots", "sale_records",
		"payment_allocations", "receipts", "receipt_sequences", "bookings",
		"documents", "document_sequences",
		"tax_payments", "tax_obligations", "tax_rates",
		"payment_schedules", "sale_contracts",
		"land_allocations", "land_sales", "land_stock_reservations", "land_stock",
		"journal_lines", "journal_entries",
		"allocation_config_versions", "allocation_configs", "allocation_executions",
		"budget_items", "budget_plans", "termin_payments",
		"unit_status_transitions", "units", "product_types", "project_phases", "projects",
		"customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", cxTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func cxSeedAccounts(t *testing.T, db *gorm.DB) map[string]uint64 {
	t.Helper()
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, cxTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	accs := []ledger.Account{
		{TenantID: cxTenant, Code: "1-1300", Name: "Bank — BCA", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
		{TenantID: cxTenant, Code: "2-2000", Name: "Uang Muka Penjualan", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "2-2100", Name: "Titipan Booking", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "2-2200", Name: "Hutang Refund", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "4-2000", Name: "Pendapatan Lain-lain", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "4-2100", Name: "Pendapatan Booking", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "1-2000", Name: "Piutang Usaha", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cxTenant, Code: "4-1000", Name: "Pendapatan Penjualan Unit", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cxTenant, Code: "5-2000", Name: "Beban PPh Final", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cxTenant, Code: "2-4000", Name: "Hutang PPh Final", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "2-1000", Name: "Hutang Usaha", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cxTenant, Code: "1-3000", Name: "Persediaan Tanah", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cxTenant, Code: "1-3100", Name: "Persediaan Hard", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cxTenant, Code: "1-3200", Name: "Persediaan Soft", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cxTenant, Code: "1-3300", Name: "Persediaan Financing", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
	}
	ids := map[string]uint64{}
	for i := range accs {
		if err := db.Create(&accs[i]).Error; err != nil {
			t.Fatalf("seed account %s: %v", accs[i].Code, err)
		}
		ids[accs[i].Code] = accs[i].ID
	}
	return ids
}

// cxNet: saldo (Σdebit − Σkredit) akun dari POSTED lines (debit-normal view).
func cxNet(t *testing.T, db *gorm.DB, code string) string {
	t.Helper()
	var s struct{ Bal string }
	if err := db.Raw(`
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`, cxTenant, code).
		Scan(&s).Error; err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return s.Bal
}

func cxSeedProjectUnit(t *testing.T, db *gorm.DB, code string, area int64, status string) (projectID, unitID uint64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		cxTenant, "CX "+code, "selling").Error; err != nil {
		t.Fatal(err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)
	// Product Catalog (fail-closed): unit_type WAJIB terdaftar di katalog.
	// Kategori 'property' + akun 4-1000 = perilaku fallback lama, jadi jurnal
	// yang dihasilkan fixture ini identik dengan sebelum hardening.
	db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, cxTenant, "villa", "villa", "property", "4-1000")
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`, cxTenant, projectID, code, "villa", domain.FromInt(area), domain.FromInt(0), status).Error; err != nil {
		t.Fatal(err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitID)
	return
}

// cxReceiptAdapter — bentuk yang sama dengan receiptGenAdapter di cmd/api/main.go.
type cxReceiptAdapter struct{ svc *billing.ReceiptService }

func (a *cxReceiptAdapter) GenerateReceiptInTx(ctx context.Context, tx *gorm.DB, tenantID, createdBy, terminID, unitID uint64,
	amount domain.Money, bankAccountCode string, date time.Time, notes string) (string, uint64, error) {
	rec, err := a.svc.GenerateReceiptInTx(ctx, tx, tenantID, createdBy, terminID, unitID, amount, bankAccountCode, date, notes)
	if err != nil {
		return "", 0, err
	}
	return rec.ReceiptNumber, rec.ID, nil
}

func cxWireSale(db *gorm.DB, withTax bool) (*sale.Service, *ledger.PostingService) {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo, allocation.WithVersionStore(allocRepo))
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)
	// W-3.2: jalur kas menuntut generator kwitansi (dokumen bernomor bukti
	// jurnal kas). Wiring produksi yang sama, bukan versi lumpuh.
	brepo := billing.NewGORMRepository(db)
	saleRepo.SetReceiptTxGenerator(&cxReceiptAdapter{svc: billing.NewReceiptService(brepo, brepo, brepo)})
	if withTax {
		saleRepo.SetTaxAccruer(tax.NewGORMRepository(db, posting))
	}
	budgetSvc := budget.NewService(budget.NewGORMRepository(db), budget.NewGORMRealisasiProvider(db))
	resolver := sale.NewBudgetedHPPResolver(budgetSvc, allocSvc, saleRepo)
	resolver.SetConfigVersionSource(allocSvc)
	svc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithHPPResolver(resolver), sale.WithPaymentCommitter(saleRepo),
		sale.WithContractStore(saleRepo), sale.WithBookingStore(saleRepo))
	return svc, posting
}

// ── (1) PRA-BAST ─────────────────────────────────────────────────────────────

func TestIntegration_Cancellation_PreBAST(t *testing.T) {
	db := cxConnect(t)
	cxCleanup(t, db)
	defer cxCleanup(t, db)
	ctx := context.Background()
	_, unitID := cxSeedProjectUnit(t, db, "PRE-01", 100, "reserved")
	cxSeedAccounts(t, db)
	saleSvc, _ := cxWireSale(db, false)
	cxSvc := cancellation.NewService(db)

	// Termin 300jt (satu pintu ReceivePayment; tanpa kontrak → buyer credit ok).
	if _, err := saleSvc.ReceivePayment(ctx, cxTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, UnitID: &unitID,
		Amount: domain.FromInt(300_000_000), Date: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300",
	}); err != nil {
		t.Fatalf("ReceivePayment: %v", err)
	}

	// Request (penalti 50jt) → approve → preview → process.
	c, err := cxSvc.Request(ctx, cxTenant, cancellation.RequestInput{
		UnitID: unitID, Reason: "buyer mundur", Penalty: domain.FromInt(50_000_000),
		EventDate: time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if c.Stage != cancellation.StagePreBAST || c.ReceivedTotal.String() != "300000000" {
		t.Fatalf("request: stage=%s received=%s", c.Stage, c.ReceivedTotal)
	}
	// Satu cancellation aktif per unit.
	if _, err := cxSvc.Request(ctx, cxTenant, cancellation.RequestInput{
		UnitID: unitID, EventDate: time.Now(),
	}); !errors.Is(err, cancellation.ErrActiveCancellationExists) {
		t.Fatalf("duplikat: want ErrActiveCancellationExists, got %v", err)
	}
	// Process sebelum approve → ditolak.
	if _, err := cxSvc.Process(ctx, cxTenant, c.ID, nil); !errors.Is(err, cancellation.ErrInvalidStateTransition) {
		t.Fatalf("process pra-approve: %v", err)
	}
	if _, err := cxSvc.Approve(ctx, cxTenant, c.ID, nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	prev, err := cxSvc.Preview(ctx, cxTenant, c.ID)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if prev.RefundAmount.String() != "250000000" || len(prev.Journals) != 1 || prev.Journals[0].Purpose != "settlement" {
		t.Fatalf("preview: %+v", prev)
	}

	done, err := cxSvc.Process(ctx, cxTenant, c.ID, nil)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if done.Status != cancellation.StatusProcessed || done.RefundID == nil || done.SettlementJournalID == nil {
		t.Fatalf("processed: %+v", done)
	}

	// Ledger: 2-2000 unit netto 0; penalti 50jt di 4-2000; hutang refund 250jt.
	if bal := cxNet(t, db, "2-2000"); bal != "0.0000" {
		t.Errorf("2-2000 = %s, want 0.0000", bal)
	}
	if bal := cxNet(t, db, "4-2000"); bal != "-50000000.0000" {
		t.Errorf("4-2000 = %s, want -50000000.0000 (kredit)", bal)
	}
	if bal := cxNet(t, db, "2-2200"); bal != "-250000000.0000" {
		t.Errorf("2-2200 = %s, want -250000000.0000 (kredit)", bal)
	}
	// Unit available + log reservation_cancelled ref cancellation.
	var st string
	db.Raw("SELECT status FROM units WHERE id=?", unitID).Scan(&st)
	if st != "available" {
		t.Errorf("unit = %s, want available", st)
	}
	var lg struct {
		Event         string
		ReferenceType string
	}
	db.Raw(`SELECT event, reference_type FROM unit_status_transitions
		WHERE tenant_id=? AND unit_id=? ORDER BY id DESC LIMIT 1`, cxTenant, unitID).Scan(&lg)
	if lg.Event != "reservation_cancelled" || lg.ReferenceType != "cancellation" {
		t.Errorf("log = %+v", lg)
	}

	// Refund → pay.
	rf, err := cxSvc.PayRefund(ctx, cxTenant, *done.RefundID, "1-1300",
		time.Date(2026, 7, 12, 0, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatalf("PayRefund: %v", err)
	}
	if rf.Status != cancellation.RefundPaid || rf.PaymentJournalID == nil {
		t.Fatalf("refund: %+v", rf)
	}
	if bal := cxNet(t, db, "2-2200"); bal != "0.0000" {
		t.Errorf("2-2200 pasca-bayar = %s, want 0.0000", bal)
	}
	// Bank: masuk 300, keluar 250 → 50 (= penalti yang ditahan).
	if bal := cxNet(t, db, "1-1300"); bal != "50000000.0000" {
		t.Errorf("bank = %s, want 50000000.0000", bal)
	}
	// Double pay ditolak.
	if _, err := cxSvc.PayRefund(ctx, cxTenant, *done.RefundID, "1-1300", time.Now(), nil); !errors.Is(err, cancellation.ErrRefundNotPending) {
		t.Fatalf("double pay: %v", err)
	}
}

// ── (2) PASCA-BAST + TRUE-UP (INV-COGS-SUM penuh) ────────────────────────────

func TestIntegration_Cancellation_PostBAST_WithTrueup(t *testing.T) {
	db := cxConnect(t)
	cxCleanup(t, db)
	defer cxCleanup(t, db)
	ctx := context.Background()

	projectID, unitA := cxSeedProjectUnit(t, db, "PB-A", 100, "reserved") // 25%
	var unitB uint64
	db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`, cxTenant, projectID, "PB-B", "villa", domain.FromInt(300), domain.FromInt(0), "reserved")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitB)
	accIDs := cxSeedAccounts(t, db)
	if err := tax.SeedDefaultRates(ctx, db, cxTenant); err != nil {
		t.Fatalf("seed tax rates: %v", err)
	}
	saleSvc, posting := cxWireSale(db, true)
	closingSvc := closing.NewService(db)
	saleSvc.SetFinalizedHPPSource(closingSvc)
	cxSvc := cancellation.NewService(db)

	// RAB land 400jt + basis + biaya aktual 480jt.
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo, allocation.WithVersionStore(allocRepo))
	budgetSvc := budget.NewService(budget.NewGORMRepository(db), budget.NewGORMRealisasiProvider(db))
	plan, err := budgetSvc.CreatePlan(ctx, cxTenant, budget.CreatePlanRequest{ProjectID: projectID, Label: "RAB"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budgetSvc.AddItem(ctx, cxTenant, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryLand, BudgetedAmount: domain.FromInt(400_000_000), Description: "land",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := budgetSvc.ApprovePlan(ctx, cxTenant, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := allocSvc.SetBasis(ctx, cxTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatal(err)
	}
	pid := projectID
	entry, err := posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: cxTenant, Date: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Description: "Biaya tanah aktual",
		Lines: []ledger.LineInput{
			{AccountID: accIDs["1-3000"], Debit: domain.FromInt(480_000_000), ProjectID: &pid},
			{AccountID: accIDs["2-1000"], Credit: domain.FromInt(480_000_000), ProjectID: &pid},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := posting.Post(ctx, cxTenant, entry.ID); err != nil {
		t.Fatal(err)
	}

	// Termin A 500jt → BAST A 2M (budgeted 100jt + PPh 2.5%=50jt accrual).
	if _, err := saleSvc.ReceivePayment(ctx, cxTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceUnitTermin, UnitID: &unitA,
		Amount: domain.FromInt(500_000_000), Date: time.Date(2026, 6, 10, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300",
	}); err != nil {
		t.Fatalf("termin A: %v", err)
	}
	if _, err := saleSvc.RecordAkad(ctx, cxTenant, sale.RecordBASTRequest{
		UnitID: unitA, SalePrice: domain.FromInt(2_000_000_000),
		BASTDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("BAST A: %v", err)
	}

	// Completion → finalize → calculate → approve → post (true-up A +20jt).
	if _, err := closingSvc.MarkCompleted(ctx, cxTenant, projectID, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := closingSvc.FinalizeCompletion(ctx, cxTenant, projectID, nil); err != nil {
		t.Fatal(err)
	}
	run, err := closingSvc.Calculate(ctx, cxTenant, projectID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := closingSvc.Approve(ctx, cxTenant, run.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := closingSvc.Post(ctx, cxTenant, run.ID, time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatal(err)
	}
	// Sanity pasca true-up: 5-1000 = 120jt (A), 1-3000 = 360jt.
	if bal := cxNet(t, db, "5-1000"); bal != "120000000.0000" {
		t.Fatalf("pre-cancel 5-1000 = %s", bal)
	}

	// ── Cancel A (penalti 100jt) ─────────────────────────────────────────────
	c, err := cxSvc.Request(ctx, cxTenant, cancellation.RequestInput{
		UnitID: unitA, Reason: "wanprestasi", Penalty: domain.FromInt(100_000_000),
		EventDate: time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if c.Stage != cancellation.StagePostBAST {
		t.Fatalf("stage = %s, want post_bast", c.Stage)
	}
	if _, err := cxSvc.Approve(ctx, cxTenant, c.ID, nil); err != nil {
		t.Fatal(err)
	}
	prev, err := cxSvc.Preview(ctx, cxTenant, c.ID)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	// received = 500jt (Event3 mengembalikan uang muka); COGS reversed = 120jt
	// (Event4 100 + true-up 20); revenue 2M; tax 50jt.
	if prev.ReceivedTotal.String() != "500000000" || prev.COGSReversed.String() != "120000000" ||
		prev.RevenueReversed.String() != "2000000000" || prev.TaxReversed.String() != "50000000" ||
		prev.RefundAmount.String() != "400000000" {
		t.Fatalf("preview: received=%s cogs=%s rev=%s tax=%s refund=%s",
			prev.ReceivedTotal, prev.COGSReversed, prev.RevenueReversed, prev.TaxReversed, prev.RefundAmount)
	}

	done, err := cxSvc.Process(ctx, cxTenant, c.ID, nil)
	if err != nil {
		t.Fatalf("Process: %v", err)
	}
	if done.RevenueReversalJournalID == nil || done.COGSReversalJournalID == nil ||
		done.TaxReversalJournalID == nil || done.SettlementJournalID == nil || done.RefundID == nil {
		t.Fatalf("jejak jurnal tidak lengkap: %+v", done)
	}

	// INV-COGS-SUM: seluruh COGS unit dibalik → 5-1000 = 0.
	if bal := cxNet(t, db, "5-1000"); bal != "0.0000" {
		t.Errorf("5-1000 = %s, want 0.0000 (INV-COGS-SUM)", bal)
	}
	// Persediaan kembali penuh ke aktual: 480jt (unit kembali ke stok pada nilai aktual).
	if bal := cxNet(t, db, "1-3000"); bal != "480000000.0000" {
		t.Errorf("1-3000 = %s, want 480000000.0000", bal)
	}
	// Pendapatan bersih 0; PPh accrual bersih 0; piutang bersih 0.
	if bal := cxNet(t, db, "4-1000"); bal != "0.0000" {
		t.Errorf("4-1000 = %s, want 0.0000", bal)
	}
	if bal := cxNet(t, db, "2-4000"); bal != "0.0000" {
		t.Errorf("2-4000 = %s, want 0.0000", bal)
	}
	if bal := cxNet(t, db, "1-2000"); bal != "0.0000" {
		t.Errorf("1-2000 = %s, want 0.0000", bal)
	}
	// Settlement: penalti 100jt; hutang refund 400jt.
	if bal := cxNet(t, db, "4-2000"); bal != "-100000000.0000" {
		t.Errorf("4-2000 = %s, want -100000000.0000", bal)
	}
	if bal := cxNet(t, db, "2-2200"); bal != "-400000000.0000" {
		t.Errorf("2-2200 = %s, want -400000000.0000", bal)
	}
	// Unit available + log cancelled_post_bast; sale_record ditandai.
	var st string
	db.Raw("SELECT status FROM units WHERE id=?", unitA).Scan(&st)
	if st != "available" {
		t.Errorf("unit = %s, want available", st)
	}
	var lg struct{ Event string }
	db.Raw(`SELECT event FROM unit_status_transitions WHERE tenant_id=? AND unit_id=? ORDER BY id DESC LIMIT 1`,
		cxTenant, unitA).Scan(&lg)
	if lg.Event != "cancelled_post_bast" {
		t.Errorf("log = %+v", lg)
	}
	var cxdAt *time.Time
	db.Raw("SELECT cancelled_at FROM sale_records WHERE tenant_id=? AND unit_id=?", cxTenant, unitA).Scan(&cxdAt)
	if cxdAt == nil {
		t.Error("sale_record.cancelled_at harus terisi")
	}
	// Obligation void.
	var obst string
	db.Raw("SELECT status FROM tax_obligations WHERE tenant_id=? AND unit_id=?", cxTenant, unitA).Scan(&obst)
	if obst != "cancelled" {
		t.Errorf("obligation = %s, want cancelled", obst)
	}
}

// ── (3) BOOKING refund payout ────────────────────────────────────────────────

func TestIntegration_Refund_FromBooking(t *testing.T) {
	db := cxConnect(t)
	cxCleanup(t, db)
	defer cxCleanup(t, db)
	ctx := context.Background()
	_, unitID := cxSeedProjectUnit(t, db, "BKR-01", 100, "available")
	cxSeedAccounts(t, db)
	var customerID uint64
	db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`, cxTenant, "C-1", "Rina")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&customerID)
	saleSvc, _ := cxWireSale(db, false)
	cxSvc := cancellation.NewService(db)

	b, err := saleSvc.CreateBooking(ctx, cxTenant, sale.CreateBookingRequest{
		UnitID: unitID, CustomerID: customerID, BookingFee: domain.FromInt(10_000_000),
		Refundable: true, BankAccountCode: "1-1300",
		BookingDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ExpiryDate:  time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	// RULE KLIEN 2026-07-29: refund booking hanya ada di JALUR LEGACY (baris
	// histori held/pending_refund). Simulasikan baris legacy: fee dipindah
	// 4-2100 → 2-2100 (jurnal aux) + disposisi 'held' + refundable.
	{
		var revID, titipanID uint64
		db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = '4-2100'`, cxTenant).Scan(&revID)
		db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = '2-2100'`, cxTenant).Scan(&titipanID)
		ledgerRepo := ledger.NewGORMRepository(db)
		posting := ledger.NewPostingService(ledgerRepo, ledgerRepo)
		pid, uid := b.ProjectID, b.UnitID
		entry, jerr := posting.Create(ctx, ledger.CreateJournalRequest{
			TenantID: cxTenant, Date: b.BookingDate,
			Description: "SIMULASI legacy: fee kembali ke titipan (test)",
			Lines: []ledger.LineInput{
				{AccountID: revID, Debit: b.BookingFee, ProjectID: &pid, UnitID: &uid},
				{AccountID: titipanID, Credit: b.BookingFee, ProjectID: &pid, UnitID: &uid},
			},
		})
		if jerr != nil {
			t.Fatalf("legacyize journal: %v", jerr)
		}
		if _, jerr := posting.Post(ctx, cxTenant, entry.ID); jerr != nil {
			t.Fatalf("legacyize post: %v", jerr)
		}
		if err := db.Exec(`UPDATE bookings SET fee_disposition = 'held', refundable = 1 WHERE id = ?`, b.ID).Error; err != nil {
			t.Fatalf("legacyize booking: %v", err)
		}
	}
	if _, err := saleSvc.CancelBooking(ctx, cxTenant, b.ID, "batal", time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatalf("CancelBooking: %v", err)
	}

	rf, err := cxSvc.CreateBookingRefund(ctx, cxTenant, b.ID, nil)
	if err != nil {
		t.Fatalf("CreateBookingRefund: %v", err)
	}
	if rf.Amount.String() != "10000000" || rf.PayableJournalID == nil || rf.Payee != "Rina" {
		t.Fatalf("refund: %+v", rf)
	}
	// Reklas: 2-2100 → 0; 2-2200 = 10jt.
	if bal := cxNet(t, db, "2-2100"); bal != "0.0000" {
		t.Errorf("2-2100 = %s, want 0.0000", bal)
	}
	if bal := cxNet(t, db, "2-2200"); bal != "-10000000.0000" {
		t.Errorf("2-2200 = %s", bal)
	}
	// Duplikat ditolak.
	if _, err := cxSvc.CreateBookingRefund(ctx, cxTenant, b.ID, nil); !errors.Is(err, cancellation.ErrRefundAlreadyRequested) {
		t.Fatalf("duplikat refund: %v", err)
	}

	paid, err := cxSvc.PayRefund(ctx, cxTenant, rf.ID, "1-1300", time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatalf("PayRefund: %v", err)
	}
	if paid.Status != cancellation.RefundPaid {
		t.Fatalf("refund status = %s", paid.Status)
	}
	if bal := cxNet(t, db, "2-2200"); bal != "0.0000" {
		t.Errorf("2-2200 pasca-bayar = %s", bal)
	}
	// Bank netto 0 (masuk 10jt, keluar 10jt); disposisi booking → refunded.
	if bal := cxNet(t, db, "1-1300"); bal != "0.0000" {
		t.Errorf("bank = %s, want 0.0000", bal)
	}
	var disp string
	db.Raw("SELECT fee_disposition FROM bookings WHERE id=?", b.ID).Scan(&disp)
	if disp != "refunded" {
		t.Errorf("disposisi = %s, want refunded", disp)
	}
}
