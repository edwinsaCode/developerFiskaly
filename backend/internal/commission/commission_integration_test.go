//go:build integration

package commission_test

// Increment 9 — integration (real MySQL): siklus penuh komisi + clawback.
//  Rule 2.5% at_bast → BAST 2M (salesperson ter-atribusi) → calculate (50jt,
//  idempoten) → approve → make-payable (Dr 5-3100/Cr 2-6200) → pay (Dr 2-6200/
//  Cr Bank) → sale dibatalkan (cancellation) → sync-cancellations → CLAWBACK
//  (Dr 1-2100/Cr 5-3100) → saldo bersih benar. Plus jalur cancel pra-bayar
//  (reversing akrual).
// Prasyarat: TEST_DB_DSN + migrasi ≥ 000046.

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/document"
	"esaproperti/internal/allocation"
	"esaproperti/internal/cancellation"
	"esaproperti/internal/commission"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

const cmTenant uint64 = 9_900_010

func cmConnect(t *testing.T) *gorm.DB {
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

func cmCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"commissions", "commission_rules",
		"refunds", "cancellations",
		"allocation_snapshot_lines", "allocation_snapshots", "sale_records",
		"payment_allocations", "payment_schedules", "sale_contracts",
		"documents", "document_sequences", "journal_lines", "journal_entries",
		"allocation_config_versions", "allocation_configs",
		"budget_items", "budget_plans", "termin_payments",
		"unit_status_transitions", "units", "product_types", "project_phases", "projects",
		"sales_persons", "sales_teams", "customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", cmTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi jalur kas tidak bisa menerbitkan dokumen tanpa ini.
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, cmTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

func cmNet(t *testing.T, db *gorm.DB, code string) string {
	t.Helper()
	var s struct{ Bal string }
	if err := db.Raw(`
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`, cmTenant, code).
		Scan(&s).Error; err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return s.Bal
}

func TestIntegration_Commission_FullCycleWithClawback(t *testing.T) {
	db := cmConnect(t)
	cmCleanup(t, db)
	defer cmCleanup(t, db)
	ctx := context.Background()

	// ── Seed: project, 2 unit reserved, akun, salesperson, customer ──────────
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		cmTenant, "CM Project", "selling").Error; err != nil {
		t.Fatal(err)
	}
	var projectID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)
	seedUnit := func(code string) uint64 {
		// Product Catalog (fail-closed): unit_type WAJIB terdaftar di katalog.
		// Kategori 'property' + akun 4-1000 = perilaku fallback lama, jadi jurnal
		// yang dihasilkan fixture ini identik dengan sebelum hardening.
		db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
			VALUES (?,?,?,?,?,TRUE)`, cmTenant, "villa", "villa", "property", "4-1000")
		db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
			VALUES (?,?,?,?,?,?,?,?)`, cmTenant, projectID, code, "villa", domain.FromInt(100), domain.FromInt(100), domain.FromInt(0), "reserved")
		var id uint64
		db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
		return id
	}
	unitA, unitB := seedUnit("CM-A"), seedUnit("CM-B")

	accs := []ledger.Account{
		{TenantID: cmTenant, Code: "1-1300", Name: "Bank", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
		{TenantID: cmTenant, Code: "1-2000", Name: "Piutang Usaha", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-2100", Name: "Piutang Lain-lain", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "2-2000", Name: "Uang Muka", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "2-2200", Name: "Hutang Refund", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "2-6200", Name: "Utang Komisi", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "4-1000", Name: "Pendapatan Penjualan", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "4-2000", Name: "Pendapatan Lain", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "5-3100", Name: "Beban Komisi", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3000", Name: "Persediaan Tanah", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3100", Name: "Persediaan Hard", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3200", Name: "Persediaan Soft", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3300", Name: "Persediaan Financing", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
	}
	for i := range accs {
		if err := db.Create(&accs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	var spID, custID uint64
	db.Exec(`INSERT INTO sales_persons (tenant_id, code, name, is_active) VALUES (?,?,?,1)`, cmTenant, "SP-1", "Rina Sales")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&spID)
	db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`, cmTenant, "C-1", "Budi")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&custID)

	// ── Wiring sale (legacy contract path — tanpa scheme flow) ───────────────
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)
	saleSvc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo))
	cmSvc := commission.NewService(db)
	cxSvc := cancellation.NewService(db)
	// Basis alokasi (prasyarat GetUnitCost jalur HPP actual; tanpa biaya → HPP 0).
	if err := allocSvc.SetBasis(ctx, cmTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatal(err)
	}

	// Kontrak ber-salesperson utk kedua unit. Catatan: jalur legacy (tanpa
	// scheme flow) tidak menyalin atribusi ke kolom — di produksi scheme flow
	// aktif dan mengisinya; di fixture kita set langsung (atribusi = prasyarat).
	mkContract := func(unitID uint64) {
		c, err := saleSvc.CreateContract(ctx, cmTenant, sale.CreateContractRequest{
			UnitID: unitID, BuyerName: "Budi", PaymentType: sale.PaymentTypeTunai,
			ContractDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			TotalPrice:   domain.FromInt(2_000_000_000),
			CustomerID:   &custID, SalesPersonID: &spID,
		})
		if err != nil {
			t.Fatalf("contract unit %d: %v", unitID, err)
		}
		if err := db.Exec(`UPDATE sale_contracts SET sales_person_id=?, customer_id=? WHERE id=?`,
			spID, custID, c.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	mkContract(unitA)
	mkContract(unitB)

	// BAST kedua unit (2M masing-masing; tanpa RAB → HPP actual 0, tanpa pajak).
	for _, u := range []uint64{unitA, unitB} {
		if _, err := saleSvc.RecordAkad(ctx, cmTenant, sale.RecordBASTRequest{
			UnitID: u, SalePrice: domain.FromInt(2_000_000_000),
			BASTDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("BAST %d: %v", u, err)
		}
	}

	// ── Rule 2.5% at_bast ────────────────────────────────────────────────────
	if _, err := cmSvc.CreateRule(ctx, cmTenant, &commission.Rule{
		Name: "Komisi standar 2.5%", Basis: commission.BasisPercentOfSale,
		Rate: decimal.RequireFromString("0.025"), TriggerEvent: commission.TriggerAtBAST,
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateRule: %v", err)
	}
	// Seam guard: tiered ditolak.
	if _, err := cmSvc.CreateRule(ctx, cmTenant, &commission.Rule{
		Name: "Tiered", Basis: commission.BasisTiered,
		EffectiveFrom: time.Now(),
	}); err == nil {
		t.Fatal("tiered harus ditolak (seam)")
	}

	// ── Calculate: 2 entri (50jt each); sweep kedua idempoten ────────────────
	n, err := cmSvc.Calculate(ctx, cmTenant, nil)
	if err != nil || n != 2 {
		t.Fatalf("Calculate: n=%d err=%v (want 2)", n, err)
	}
	if n, _ := cmSvc.Calculate(ctx, cmTenant, nil); n != 0 {
		t.Fatalf("Calculate kedua = %d, want 0 (idempoten)", n)
	}
	list, _ := cmSvc.List(ctx, cmTenant, commission.StatusCalculated, 0)
	if len(list) != 2 || list[0].Amount.String() != "50000000" {
		t.Fatalf("entries: %+v", list)
	}
	// Ambil per unit.
	var cmA, cmB *commission.Commission
	for _, c := range list {
		if c.UnitID == unitA {
			cmA = c
		} else {
			cmB = c
		}
	}

	// ── Lifecycle A: approve → payable → pay ─────────────────────────────────
	if _, err := cmSvc.Approve(ctx, cmTenant, cmA.ID, nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	// Guard: pay sebelum payable.
	if _, err := cmSvc.Pay(ctx, cmTenant, cmA.ID, "1-1300", time.Now(), nil); err == nil {
		t.Fatal("pay pra-payable harus ditolak")
	}
	pa, err := cmSvc.MakePayable(ctx, cmTenant, cmA.ID, time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), nil)
	if err != nil || pa.AccrualJournalID == nil {
		t.Fatalf("MakePayable: %v %+v", err, pa)
	}
	if bal := cmNet(t, db, "5-3100"); bal != "50000000.0000" {
		t.Errorf("5-3100 pasca-akrual = %s", bal)
	}
	if bal := cmNet(t, db, "2-6200"); bal != "-50000000.0000" {
		t.Errorf("2-6200 pasca-akrual = %s", bal)
	}
	paid, err := cmSvc.Pay(ctx, cmTenant, cmA.ID, "1-1300", time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC), nil)
	if err != nil || paid.Status != commission.StatusPaid {
		t.Fatalf("Pay: %v", err)
	}
	if bal := cmNet(t, db, "2-6200"); bal != "0.0000" {
		t.Errorf("2-6200 pasca-bayar = %s", bal)
	}
	if bal := cmNet(t, db, "1-1300"); bal != "-50000000.0000" {
		t.Errorf("bank = %s (keluar 50jt)", bal)
	}

	// ── Lifecycle B: approve → payable → CANCEL (reversing akrual) ───────────
	if _, err := cmSvc.Approve(ctx, cmTenant, cmB.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cmSvc.MakePayable(ctx, cmTenant, cmB.ID, time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cmSvc.Cancel(ctx, cmTenant, cmB.ID, "koreksi", nil); err != nil {
		t.Fatalf("Cancel B: %v", err)
	}
	// Akrual B terbalik: beban kembali 50jt (hanya A), utang 0.
	if bal := cmNet(t, db, "5-3100"); bal != "50000000.0000" {
		t.Errorf("5-3100 pasca-cancel B = %s, want 50000000.0000", bal)
	}
	if bal := cmNet(t, db, "2-6200"); bal != "0.0000" {
		t.Errorf("2-6200 pasca-cancel B = %s", bal)
	}

	// ── Clawback: batalkan sale A (cancellation) → sync → clawed_back ────────
	cx, err := cxSvc.Request(ctx, cmTenant, cancellation.RequestInput{
		UnitID: unitA, Reason: "buyer batal", Penalty: domain.Zero,
		EventDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("cx request: %v", err)
	}
	if _, err := cxSvc.Approve(ctx, cmTenant, cx.ID, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := cxSvc.Process(ctx, cmTenant, cx.ID, nil); err != nil {
		t.Fatalf("cx process: %v", err)
	}

	cancelledN, clawedN, err := cmSvc.SyncCancellations(ctx, cmTenant, nil)
	if err != nil || clawedN != 1 || cancelledN != 0 {
		t.Fatalf("sync: cancelled=%d clawed=%d err=%v (want 0/1)", cancelledN, clawedN, err)
	}
	gA, _ := cmSvc.Get(ctx, cmTenant, cmA.ID)
	if gA.Status != commission.StatusClawedBack || gA.ClawbackJournalID == nil {
		t.Fatalf("A = %+v, want clawed_back + jurnal", gA)
	}
	// Clawback: beban komisi pulih 0; piutang clawback 50jt dari sales.
	if bal := cmNet(t, db, "5-3100"); bal != "0.0000" {
		t.Errorf("5-3100 pasca-clawback = %s, want 0.0000", bal)
	}
	if bal := cmNet(t, db, "1-2100"); bal != "50000000.0000" {
		t.Errorf("1-2100 = %s, want 50000000.0000 (piutang dari sales)", bal)
	}
	// Sync idempoten.
	if c2, cl2, _ := cmSvc.SyncCancellations(ctx, cmTenant, nil); c2 != 0 || cl2 != 0 {
		t.Errorf("sync kedua: %d/%d, want 0/0", c2, cl2)
	}
}

// Task 6 — Komisi Sales & Marketing (Rupiah nominal): rule flat_per_unit
// harus menghasilkan nominal tetap TERLEPAS dari harga jual (bukan
// dihitung sebagai persentase), berdampingan dengan rule percent_of_sale
// existing tanpa regresi (backward-compatible).
func TestIntegration_Commission_FlatPerUnitCoexistsWithPercent(t *testing.T) {
	db := cmConnect(t)
	cmCleanup(t, db)
	defer cmCleanup(t, db)
	ctx := context.Background()

	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		cmTenant, "CM Flat Project", "selling").Error; err != nil {
		t.Fatal(err)
	}
	var projectID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)
	seedUnit := func(code string) uint64 {
		db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
			VALUES (?,?,?,?,?,TRUE)`, cmTenant, "villa", "villa", "property", "4-1000")
		db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
			VALUES (?,?,?,?,?,?,?,?)`, cmTenant, projectID, code, "villa", domain.FromInt(100), domain.FromInt(100), domain.FromInt(0), "reserved")
		var id uint64
		db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
		return id
	}
	unitPct, unitFlat := seedUnit("CF-PCT"), seedUnit("CF-FLAT")

	accs := []ledger.Account{
		{TenantID: cmTenant, Code: "1-1300", Name: "Bank", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
		{TenantID: cmTenant, Code: "1-2000", Name: "Piutang Usaha", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "2-2000", Name: "Uang Muka", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "2-6200", Name: "Utang Komisi", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "4-1000", Name: "Pendapatan Penjualan", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: cmTenant, Code: "5-3100", Name: "Beban Komisi", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3000", Name: "Persediaan Tanah", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3100", Name: "Persediaan Hard", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3200", Name: "Persediaan Soft", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: cmTenant, Code: "1-3300", Name: "Persediaan Financing", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
	}
	for i := range accs {
		if err := db.Create(&accs[i]).Error; err != nil {
			t.Fatal(err)
		}
	}
	var spPct, spFlat, custID uint64
	db.Exec(`INSERT INTO sales_persons (tenant_id, code, name, is_active) VALUES (?,?,?,1)`, cmTenant, "SP-PCT", "Sales Persen")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&spPct)
	db.Exec(`INSERT INTO sales_persons (tenant_id, code, name, is_active) VALUES (?,?,?,1)`, cmTenant, "SP-FLAT", "Sales Flat")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&spFlat)
	db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`, cmTenant, "C-1", "Budi")
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&custID)

	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)
	saleSvc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithContractStore(saleRepo))
	cmSvc := commission.NewService(db)
	if err := allocSvc.SetBasis(ctx, cmTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatal(err)
	}

	// Kedua unit dijual di HARGA SAMA (2M) — membuktikan hasil flat TIDAK
	// mengikuti harga (kalau salah dihitung sebagai persentase, hasilnya akan
	// beda dari nominal rule dan/atau ikut naik-turun bersama harga).
	const salePrice = 2_000_000_000
	mkContract := func(unitID, spID uint64) {
		c, err := saleSvc.CreateContract(ctx, cmTenant, sale.CreateContractRequest{
			UnitID: unitID, BuyerName: "Budi", PaymentType: sale.PaymentTypeTunai,
			ContractDate: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
			TotalPrice:   domain.FromInt(salePrice),
			CustomerID:   &custID, SalesPersonID: &spID,
		})
		if err != nil {
			t.Fatalf("contract unit %d: %v", unitID, err)
		}
		if err := db.Exec(`UPDATE sale_contracts SET sales_person_id=?, customer_id=? WHERE id=?`,
			spID, custID, c.ID).Error; err != nil {
			t.Fatal(err)
		}
	}
	mkContract(unitPct, spPct)
	mkContract(unitFlat, spFlat)
	for _, u := range []uint64{unitPct, unitFlat} {
		if _, err := saleSvc.RecordAkad(ctx, cmTenant, sale.RecordBASTRequest{
			UnitID: u, SalePrice: domain.FromInt(salePrice),
			BASTDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
		}); err != nil {
			t.Fatalf("BAST %d: %v", u, err)
		}
	}

	// Rule percent existing (scoped ke spPct) — harus tetap 2.5% dari harga.
	if _, err := cmSvc.CreateRule(ctx, cmTenant, &commission.Rule{
		Name: "Komisi standar 2.5%", Basis: commission.BasisPercentOfSale,
		Rate: decimal.RequireFromString("0.025"), TriggerEvent: commission.TriggerAtBAST,
		SalesPersonID: &spPct,
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateRule percent: %v", err)
	}
	// Rule baru — Nominal Rupiah tetap (Task 6), scoped ke spFlat.
	if _, err := cmSvc.CreateRule(ctx, cmTenant, &commission.Rule{
		Name: "Komisi flat Rp2.5jt", Basis: commission.BasisFlatPerUnit,
		FlatAmount: domain.FromInt(2_500_000), TriggerEvent: commission.TriggerAtBAST,
		SalesPersonID: &spFlat,
		EffectiveFrom: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("CreateRule flat: %v", err)
	}
	// Guard: flat_amount 0/negatif ditolak (uang tak boleh sembarangan).
	if _, err := cmSvc.CreateRule(ctx, cmTenant, &commission.Rule{
		Name: "Flat invalid", Basis: commission.BasisFlatPerUnit,
		FlatAmount: domain.Zero, TriggerEvent: commission.TriggerAtBAST,
		EffectiveFrom: time.Now(),
	}); err == nil {
		t.Fatal("flat_amount nol harus ditolak")
	}

	n, err := cmSvc.Calculate(ctx, cmTenant, nil)
	if err != nil || n != 2 {
		t.Fatalf("Calculate: n=%d err=%v (want 2)", n, err)
	}
	if n, _ := cmSvc.Calculate(ctx, cmTenant, nil); n != 0 {
		t.Fatalf("Calculate kedua = %d, want 0 (idempoten)", n)
	}

	list, _ := cmSvc.List(ctx, cmTenant, commission.StatusCalculated, 0)
	if len(list) != 2 {
		t.Fatalf("entries: %+v", list)
	}
	var cPct, cFlat *commission.Commission
	for _, c := range list {
		if c.UnitID == unitPct {
			cPct = c
		} else {
			cFlat = c
		}
	}
	if cPct == nil || cPct.Amount.String() != "50000000" {
		t.Fatalf("percent entry salah: %+v (want 50000000, 2.5%% dari %d)", cPct, salePrice)
	}
	if cFlat == nil || cFlat.Amount.String() != "2500000" {
		t.Fatalf("flat entry salah: %+v (want nominal tetap 2500000, BUKAN persentase harga)", cFlat)
	}

	// Lifecycle flat sampai payable+pay — buktikan jurnal pakai nominal Rupiah
	// yang di-set admin, bukan hasil kali rate (rate_snapshot flat = 0).
	if cFlat.RateSnapshot.Sign() != 0 {
		t.Errorf("rate_snapshot rule flat = %s, want 0 (basis bukan persentase)", cFlat.RateSnapshot)
	}
	if _, err := cmSvc.Approve(ctx, cmTenant, cFlat.ID, nil); err != nil {
		t.Fatalf("Approve flat: %v", err)
	}
	if _, err := cmSvc.MakePayable(ctx, cmTenant, cFlat.ID, time.Date(2026, 6, 20, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatalf("MakePayable flat: %v", err)
	}
	if bal := cmNet(t, db, "5-3100"); bal != "2500000.0000" {
		t.Errorf("5-3100 pasca-akrual flat = %s, want 2500000.0000", bal)
	}
	paid, err := cmSvc.Pay(ctx, cmTenant, cFlat.ID, "1-1300", time.Date(2026, 6, 25, 0, 0, 0, 0, time.UTC), nil)
	if err != nil || paid.Amount.String() != "2500000" {
		t.Fatalf("Pay flat: %v %+v", err, paid)
	}
	if bal := cmNet(t, db, "1-1300"); bal != "-2500000.0000" {
		t.Errorf("bank keluar = %s, want -2500000.0000 (nominal tetap, bukan persentase)", bal)
	}
}
