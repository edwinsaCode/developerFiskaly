//go:build integration

package land_test

// LT-5 — integration test land.GORMRepository.RecordAkad terhadap MySQL
// nyata. Membuktikan: jurnal Akad (pendapatan+HPP) terposting balanced
// (Σdebit==Σkredit, invariant #1), land_stock.sold_quantity_m2 ter-update
// atomik di bawah row-lock, reservasi (jika ada) transisi ke converted
// dengan converted_sale_id benar, land_allocations SELALU dibuat 1:1 (R-2)
// untuk kedua metode HPP, dan isolasi tenant.
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (s/d 000081).

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/document"
	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
)

func ltMustAccount(t *testing.T, db *gorm.DB, tenantID uint64, code, name string, typ domain.AccountType, nb domain.NormalBalance, category ledger.AccountCategory) uint64 {
	t.Helper()
	acc := ledger.Account{
		TenantID: tenantID, Code: code, Name: name, Type: typ, NormalBalance: nb, IsActive: true, Category: category,
	}
	if err := db.WithContext(context.Background()).Create(&acc).Error; err != nil {
		t.Fatalf("seed account %s: %v", code, err)
	}
	return acc.ID
}

// ltSeedCOA seeds the fixed 5-account chart RecordAkad needs: kas/bank
// tujuan, pendapatan (4-1100), PPN keluaran (2-3000), HPP (5-1000),
// persediaan tanah (1-3000) — kode akun sesuai
// kelebihan-tanah-final-architecture-2026-08.md §D (2-3000 mengoreksi typo
// draf 2-1300, konsisten dengan sale package).
func ltSeedCOA(t *testing.T, db *gorm.DB, tenantID uint64) map[string]uint64 {
	t.Helper()
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, tenantID); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	ids := map[string]uint64{}
	ids["1-1300"] = ltMustAccount(t, db, tenantID, "1-1300", "Bank — BCA", domain.AccountAsset, domain.NormalBalanceDebit, ledger.CategoryBank)
	ids["4-1100"] = ltMustAccount(t, db, tenantID, "4-1100", "Pendapatan Kelebihan Tanah", domain.AccountRevenue, domain.NormalBalanceCredit, "")
	ids["2-3000"] = ltMustAccount(t, db, tenantID, "2-3000", "PPN Keluaran", domain.AccountLiability, domain.NormalBalanceCredit, "")
	ids["5-1000"] = ltMustAccount(t, db, tenantID, "5-1000", "HPP", domain.AccountExpense, domain.NormalBalanceDebit, "")
	ids["1-3000"] = ltMustAccount(t, db, tenantID, "1-3000", "Persediaan Tanah", domain.AccountAsset, domain.NormalBalanceDebit, ledger.CategoryOtherAsset)
	return ids
}

// ltCleanupAkad extends ltCleanup with the LT-5 tables. Circular FK between
// land_sales (reservation_id → land_stock_reservations) and
// land_stock_reservations (converted_sale_id → land_sales) requires nulling
// converted_sale_id before either table can be deleted.
func ltCleanupAkad(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tenantID := range []uint64{ltTenant, ltOtherTenant} {
		if err := db.Exec("DELETE FROM land_allocations WHERE tenant_id = ?", tenantID).Error; err != nil {
			t.Fatalf("cleanup land_allocations: %v", err)
		}
		if err := db.Exec("UPDATE land_stock_reservations SET converted_sale_id = NULL WHERE tenant_id = ?", tenantID).Error; err != nil {
			t.Fatalf("cleanup land_stock_reservations.converted_sale_id: %v", err)
		}
		if err := db.Exec("DELETE FROM land_sales WHERE tenant_id = ?", tenantID).Error; err != nil {
			t.Fatalf("cleanup land_sales: %v", err)
		}
	}
	ltCleanup(t, db) // land_stock_reservations, land_stock, customers, projects
	for _, tenantID := range []uint64{ltTenant, ltOtherTenant} {
		for _, tbl := range []string{"journal_lines", "journal_entries", "documents", "document_sequences", "document_types", "accounts"} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", tenantID).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tbl, err)
			}
		}
	}
}

// ltNet: saldo (Σdebit − Σkredit) akun dari POSTED lines (debit-normal view).
func ltNet(t *testing.T, db *gorm.DB, tenantID uint64, code string) decimal.Decimal {
	t.Helper()
	var s struct{ Bal string }
	if err := db.Raw(`
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`, tenantID, code).
		Scan(&s).Error; err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return decimal.RequireFromString(s.Bal)
}

// ltJournalBalanced asserts Σdebit==Σkredit for a posted journal entry
// (invariant #1, enforced independently of the app-level posting guard).
func ltJournalBalanced(t *testing.T, db *gorm.DB, entryID uint64) {
	t.Helper()
	var s struct{ D, C string }
	if err := db.Raw(`SELECT COALESCE(SUM(debit),0) AS d, COALESCE(SUM(credit),0) AS c FROM journal_lines WHERE journal_entry_id = ?`, entryID).
		Scan(&s).Error; err != nil {
		t.Fatalf("sum journal_lines %d: %v", entryID, err)
	}
	d, c := decimal.RequireFromString(s.D), decimal.RequireFromString(s.C)
	if !d.Equal(c) {
		t.Fatalf("journal %d tidak balanced: debit=%s kredit=%s", entryID, d, c)
	}
}

func ltAkadParams(accs map[string]uint64, projectID, landStockID, customerID uint64, qty, dpp, gross, hppTotal string) land.RecordAkadParams {
	q := decimal.RequireFromString(qty)
	dppM := domain.FromDecimal(decimal.RequireFromString(dpp))
	grossM := domain.FromDecimal(decimal.RequireFromString(gross))
	hppM := domain.FromDecimal(decimal.RequireFromString(hppTotal))
	var cogs []land.JournalLineInput
	if !hppM.IsZero() {
		cogs = []land.JournalLineInput{
			{AccountID: accs["5-1000"], Debit: hppM, Credit: domain.Zero, ProjectID: &projectID, Description: "HPP Akad Kelebihan Tanah"},
			{AccountID: accs["1-3000"], Debit: domain.Zero, Credit: hppM, ProjectID: &projectID, Description: "HPP Akad Kelebihan Tanah"},
		}
	}
	return land.RecordAkadParams{
		ProjectID: projectID, LandStockID: landStockID, CustomerID: customerID,
		QuantityM2: q, UnitPriceSnapshot: domain.FromInt(1_000_000),
		DPPAmount: dppM, GrossAmount: grossM,
		PaymentAccountCode: "1-1300", RecognitionDate: time.Now(),
		RevenueLines: []land.JournalLineInput{
			{AccountID: accs["1-1300"], Debit: grossM, Credit: domain.Zero, ProjectID: &projectID, Description: "Akad Kelebihan Tanah"},
			{AccountID: accs["4-1100"], Debit: domain.Zero, Credit: dppM, ProjectID: &projectID, Description: "Akad Kelebihan Tanah"},
		},
		COGSLines:    cogs,
		HPPRatePerM2: domain.FromInt(600_000),
		HPPTotal:     hppM,
		Basis:        "land_area",
	}
}

func TestIntegration_RecordAkad_Success(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Akad Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-AKAD-1")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	params := ltAkadParams(accs, projectID, pool.ID, custID, "100", "100000000", "100000000", "60000000")
	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if sale.Status != land.LandSaleStatusAkad {
		t.Errorf("status want akad, got %s", sale.Status)
	}
	if sale.RevenueJournalID == nil {
		t.Fatal("RevenueJournalID want non-nil")
	}
	if sale.CogsJournalID == nil {
		t.Fatal("CogsJournalID want non-nil (hpp_total > 0)")
	}
	ltJournalBalanced(t, db, *sale.RevenueJournalID)
	ltJournalBalanced(t, db, *sale.CogsJournalID)

	if got := ltNet(t, db, ltTenant, "1-1300"); !got.Equal(decimal.RequireFromString("100000000")) {
		t.Errorf("kas want 100000000, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "4-1100"); !got.Equal(decimal.RequireFromString("-100000000")) {
		t.Errorf("pendapatan want -100000000, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "5-1000"); !got.Equal(decimal.RequireFromString("60000000")) {
		t.Errorf("HPP want 60000000, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "1-3000"); !got.Equal(decimal.RequireFromString("-60000000")) {
		t.Errorf("persediaan tanah want -60000000, got %s", got)
	}

	gotPool, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !gotPool.SoldQuantityM2.Equal(decimal.NewFromInt(100)) {
		t.Errorf("sold_quantity_m2 want 100, got %s", gotPool.SoldQuantityM2)
	}

	var alloc land.LandAllocation
	if err := db.Where("tenant_id = ? AND land_sale_id = ?", ltTenant, sale.ID).First(&alloc).Error; err != nil {
		t.Fatalf("land_allocations wajib ada 1:1 (R-2): %v", err)
	}
	if alloc.Basis != "land_area" {
		t.Errorf("basis want land_area, got %s", alloc.Basis)
	}
	if !alloc.HPPTotal.Decimal().Equal(decimal.RequireFromString("60000000")) {
		t.Errorf("hpp_total snapshot want 60000000, got %s", alloc.HPPTotal.Decimal())
	}

	got, err := repo.FindLandSale(context.Background(), ltTenant, sale.ID)
	if err != nil {
		t.Fatalf("FindLandSale: %v", err)
	}
	if got.ID != sale.ID {
		t.Errorf("FindLandSale roundtrip mismatch")
	}
}

func TestIntegration_RecordAkad_PPN_JournalAmounts(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Akad PPN Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-AKAD-PPN")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	// DPP 50jt × 11% = 5.5jt PPN → gross 55.5jt.
	params := ltAkadParams(accs, projectID, pool.ID, custID, "50", "50000000", "55500000", "0")
	params.IsPKP = true
	params.VATRateSnapshot = decimal.RequireFromString("0.11")
	params.RevenueLines = []land.JournalLineInput{
		{AccountID: accs["1-1300"], Debit: domain.FromInt(55_500_000), Credit: domain.Zero, ProjectID: &projectID},
		{AccountID: accs["4-1100"], Debit: domain.Zero, Credit: domain.FromInt(50_000_000), ProjectID: &projectID},
		{AccountID: accs["2-3000"], Debit: domain.Zero, Credit: domain.FromInt(5_500_000), ProjectID: &projectID},
	}

	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if sale.CogsJournalID != nil {
		t.Errorf("CogsJournalID want nil (hpp_total=0), got %v", *sale.CogsJournalID)
	}
	ltJournalBalanced(t, db, *sale.RevenueJournalID)

	if got := ltNet(t, db, ltTenant, "1-1300"); !got.Equal(decimal.RequireFromString("55500000")) {
		t.Errorf("kas want 55500000, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "4-1100"); !got.Equal(decimal.RequireFromString("-50000000")) {
		t.Errorf("pendapatan want -50000000, got %s", got)
	}
	if got := ltNet(t, db, ltTenant, "2-3000"); !got.Equal(decimal.RequireFromString("-5500000")) {
		t.Errorf("PPN keluaran want -5500000, got %s", got)
	}
}

func TestIntegration_RecordAkad_ConvertsReservation(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Akad Reservation Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-AKAD-RES")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	res, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.NewFromInt(150), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	params := ltAkadParams(accs, projectID, pool.ID, custID, "150", "150000000", "150000000", "90000000")
	params.ReservationID = &res.ID
	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	gotRes, err := repo.FindReservation(context.Background(), ltTenant, res.ID)
	if err != nil {
		t.Fatalf("FindReservation: %v", err)
	}
	if gotRes.Status != land.ReservationStatusConverted {
		t.Errorf("reservation status want converted, got %s", gotRes.Status)
	}
	if gotRes.ConvertedSaleID == nil || *gotRes.ConvertedSaleID != sale.ID {
		t.Errorf("converted_sale_id want %d, got %v", sale.ID, gotRes.ConvertedSaleID)
	}

	gotPool, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !gotPool.ReservedQuantityM2.IsZero() {
		t.Errorf("reserved_quantity_m2 want 0 (transferred to sold), got %s", gotPool.ReservedQuantityM2)
	}
	if !gotPool.SoldQuantityM2.Equal(decimal.NewFromInt(150)) {
		t.Errorf("sold_quantity_m2 want 150, got %s", gotPool.SoldQuantityM2)
	}
	// Total committed (reserved+sold) tetap konstan melewati konversi —
	// INV-LAND-1 tidak boleh bocor kuantitas.
	if !gotPool.AvailableQuantityM2().Equal(decimal.NewFromInt(350)) {
		t.Errorf("available want 350, got %s", gotPool.AvailableQuantityM2())
	}
}

func TestIntegration_RecordAkad_ReservationQuantityMismatchRollsBack(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Akad Mismatch Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-AKAD-MISMATCH")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	res, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.NewFromInt(100), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	params := ltAkadParams(accs, projectID, pool.ID, custID, "50", "50000000", "50000000", "0")
	params.ReservationID = &res.ID
	_, err = repo.RecordAkad(context.Background(), ltTenant, params)
	if err != land.ErrReservationQuantityMismatch {
		t.Fatalf("want ErrReservationQuantityMismatch, got %v", err)
	}

	// Rollback penuh: sold_quantity_m2 tidak berubah, tidak ada jurnal baru,
	// reservasi masih active.
	gotPool, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !gotPool.SoldQuantityM2.IsZero() {
		t.Errorf("sold_quantity_m2 want 0 after rollback, got %s", gotPool.SoldQuantityM2)
	}
	gotRes, err := repo.FindReservation(context.Background(), ltTenant, res.ID)
	if err != nil {
		t.Fatalf("FindReservation: %v", err)
	}
	if gotRes.Status != land.ReservationStatusActive {
		t.Errorf("reservation status want still active after rollback, got %s", gotRes.Status)
	}
	var count int64
	if err := db.Model(&land.LandSale{}).Where("tenant_id = ?", ltTenant).Count(&count).Error; err != nil {
		t.Fatalf("count land_sales: %v", err)
	}
	if count != 0 {
		t.Errorf("land_sales want 0 rows after rollback, got %d", count)
	}
}

func TestIntegration_RecordAkad_CapacityExceededWithoutReservation(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Akad Capacity Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-AKAD-CAP")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	params := ltAkadParams(accs, projectID, pool.ID, custID, "600", "600000000", "600000000", "0")
	_, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != land.ErrCapacityExceeded {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}
}

func TestIntegration_RecordAkad_TenantIsolation(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Akad Isolation Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-AKAD-ISO")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")
	accs := ltSeedCOA(t, db, ltTenant)

	params := ltAkadParams(accs, projectID, pool.ID, custID, "80", "80000000", "80000000", "0")
	sale, err := repo.RecordAkad(context.Background(), ltTenant, params)
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	if _, err := repo.FindLandSale(context.Background(), ltOtherTenant, sale.ID); err != land.ErrLandSaleNotFound {
		t.Fatalf("cross-tenant FindLandSale want ErrLandSaleNotFound, got %v", err)
	}
	list, err := repo.ListLandSalesByProject(context.Background(), ltOtherTenant, projectID)
	if err != nil {
		t.Fatalf("ListLandSalesByProject: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("cross-tenant ListLandSalesByProject want 0 rows, got %d", len(list))
	}
}

func TestIntegration_ValidateCashBankAccount(t *testing.T) {
	db := ltConnect(t)
	ltCleanupAkad(t, db)
	defer ltCleanupAkad(t, db)

	repo := land.NewGORMRepository(db)
	ltSeedCOA(t, db, ltTenant)

	if err := repo.ValidateCashBankAccount(context.Background(), ltTenant, "1-1300"); err != nil {
		t.Errorf("ValidateCashBankAccount 1-1300 want nil, got %v", err)
	}
	if err := repo.ValidateCashBankAccount(context.Background(), ltTenant, "4-1100"); err != land.ErrInvalidBankAccount {
		t.Errorf("ValidateCashBankAccount 4-1100 (revenue, bukan kas/bank) want ErrInvalidBankAccount, got %v", err)
	}
	if err := repo.ValidateCashBankAccount(context.Background(), ltTenant, "9-9999"); err != land.ErrPaymentAccountNotFound {
		t.Errorf("ValidateCashBankAccount kode tak ada want ErrPaymentAccountNotFound, got %v", err)
	}
}
