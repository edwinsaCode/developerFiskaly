//go:build integration

package sale_test

// kelebihan-tanah-booking-integration-2026-08 — integration (real MySQL):
// membuktikan komponen Produk Tambahan Kelebihan Tanah yang menempel pada
// Booking/Kontrak/Akad unit benar-benar atomik dengan siklus hidup
// finansialnya sendiri — reservasi lahir bersama Booking, lepas bersama
// pembatalan, kelebihan kapasitas membatalkan SELURUH Booking (rollback
// penuh, bukan cuma komponen tanahnya), dan Akad membukukan jurnal
// pendapatan+HPP rumah DAN tanah dalam SATU transaksi (§D3), balanced.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (≥ 000084/000085).

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

const lbTenant uint64 = 9_900_086

func lbCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	// land_sales <-> land_stock_reservations adalah FK sirkular
	// (reservation.converted_sale_id -> land_sales, land_sales.reservation_id
	// -> reservation) — putus siklusnya dulu sebelum menghapus keduanya.
	if err := db.Exec(`UPDATE land_stock_reservations SET converted_sale_id = NULL WHERE tenant_id = ?`, lbTenant).Error; err != nil {
		t.Fatalf("cleanup putus siklus land_stock_reservations: %v", err)
	}
	for _, tbl := range []string{
		"cancellations",
		"payment_allocations", "receipts", "receipt_sequences", "bookings",
		"payment_schedules", "sale_contracts", "termin_payments",
		"documents", "document_sequences",
		"land_allocations", "land_sales", "land_stock_reservations", "land_stock",
		"journal_lines", "journal_entries",
		"unit_status_transitions", "units", "product_types", "project_phases", "projects",
		"customers", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", lbTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

// lbSeed menanam project + unit (status diserahkan pemanggil) + customer +
// katalog produk + COA lengkap (unit rumah + tanah + booking fee).
func lbSeed(t *testing.T, db *gorm.DB, unitStatus string) (projectID, unitID, customerID uint64) {
	t.Helper()
	if err := slSeedDocumentTypes(db, lbTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		lbTenant, "LB Project", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)

	db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, lbTenant, "villa", "villa", "property", "4-1000")

	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`,
		lbTenant, projectID, "LB-01", "villa", domain.FromInt(100), domain.FromInt(0), unitStatus).Error; err != nil {
		t.Fatalf("seed unit: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitID)

	if err := db.Exec(`INSERT INTO customers (tenant_id, code, name) VALUES (?,?,?)`,
		lbTenant, "CUST-LB", "Buyer LB").Error; err != nil {
		t.Fatalf("seed customer: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&customerID)

	accs := []ledger.Account{
		{TenantID: lbTenant, Code: "1-1300", Name: "Bank — BCA", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true, Category: ledger.CategoryBank},
		{TenantID: lbTenant, Code: "1-2000", Name: "Piutang Usaha", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: lbTenant, Code: "4-1000", Name: "Pendapatan Penjualan Unit", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: lbTenant, Code: "4-1100", Name: "Pendapatan Kelebihan Tanah", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: lbTenant, Code: "2-3000", Name: "PPN Keluaran", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: lbTenant, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: lbTenant, Code: "1-3000", Name: "Persediaan Tanah", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: lbTenant, Code: "1-3100", Name: "Persediaan Hard Cost", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: lbTenant, Code: "1-3200", Name: "Persediaan Soft Cost", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: lbTenant, Code: "1-3300", Name: "Persediaan Financing Cost", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: lbTenant, Code: "4-2100", Name: "Pendapatan Booking", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: lbTenant, Code: "2-2100", Name: "Titipan Booking", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: lbTenant, Code: "2-2000", Name: "Uang Muka Penjualan", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: lbTenant, Code: "4-2000", Name: "Pendapatan Lain-lain", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
	}
	for i := range accs {
		if err := db.Create(&accs[i]).Error; err != nil {
			t.Fatalf("seed account %s: %v", accs[i].Code, err)
		}
	}
	return
}

func lbMustPool(t *testing.T, db *gorm.DB, projectID uint64, total, price string) *land.LandStock {
	t.Helper()
	pool := &land.LandStock{
		TenantID: lbTenant, ProjectID: projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.RequireFromString(total), UnitPrice: domain.MustParse(price),
	}
	if err := land.NewGORMRepository(db).CreatePool(context.Background(), pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	return pool
}

// lbWire merangkai sale.Service produksi + land.Service (bundled Akad seam)
// dengan wiring minimal — pola identik bkWire, ditambah WithLandAkadPreparer.
func lbWire(db *gorm.DB, landHPP land.LandHPPResolver, unitHPP sale.HPPResolver) *sale.Service {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	repo := slWireReceipts(db, sale.NewGORMRepository(db, posting, allocSvc))
	opts := []sale.ServiceOption{sale.WithContractStore(repo), sale.WithBookingStore(repo)}
	if landHPP != nil {
		landSvc := land.NewService(land.NewGORMRepository(db), land.WithHPPResolver(landHPP))
		opts = append(opts, sale.WithLandAkadPreparer(landSvc))
	}
	if unitHPP != nil {
		opts = append(opts, sale.WithHPPResolver(unitHPP))
	}
	return sale.NewService(repo, repo, repo, repo, repo, repo, opts...)
}

type lbStubHPPResolver struct {
	res land.LandHPPResolution
	err error
}

func (r *lbStubHPPResolver) ResolveLandHPPRate(_ context.Context, _, _ uint64) (land.LandHPPResolution, error) {
	return r.res, r.err
}

// lbZeroUnitHPPResolver — HPP UNIT (bukan tanah) sengaja dibuat nol: test ini
// fokus membuktikan atomicity+balance bundel rumah+tanah pada Akad, bukan
// resolusi HPP rumah (sudah diuji terpisah di bast_budgeted_integration_test.go).
// Tanpa ini, fallback default costProvider.GetUnitCost butuh RAB/allocation
// config penuh yang di luar cakupan test ini.
type lbZeroUnitHPPResolver struct{}

func (lbZeroUnitHPPResolver) ResolveHPP(_ context.Context, _, _ uint64, _ *uint64, _ uint64) (sale.HPPResolution, error) {
	return sale.HPPResolution{Method: sale.HPPMethodActual}, nil
}

func lbBookingReq(unitID, customerID uint64, qty *decimal.Decimal) sale.CreateBookingRequest {
	return sale.CreateBookingRequest{
		UnitID:          unitID,
		CustomerID:      customerID,
		BookingFee:      domain.FromInt(5_000_000),
		Refundable:      false,
		BankAccountCode: "1-1300",
		BookingDate:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ExpiryDate:      time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
		LandQuantityM2:  qty,
	}
}

func lbJournalBalanced(t *testing.T, db *gorm.DB, journalID uint64, wantTotal string) {
	t.Helper()
	type row struct {
		Debit  domain.Money `gorm:"column:d"`
		Credit domain.Money `gorm:"column:c"`
	}
	var r row
	if err := db.Table("journal_lines").
		Select("COALESCE(SUM(debit),0) AS d, COALESCE(SUM(credit),0) AS c").
		Where("tenant_id = ? AND journal_entry_id = ?", lbTenant, journalID).Scan(&r).Error; err != nil {
		t.Fatalf("sum journal %d: %v", journalID, err)
	}
	if r.Debit.String() != r.Credit.String() {
		t.Errorf("jurnal %d tidak balanced: debit=%s credit=%s", journalID, r.Debit, r.Credit)
	}
	if wantTotal != "" && r.Debit.String() != wantTotal {
		t.Errorf("jurnal %d total debit: got %s, want %s", journalID, r.Debit, wantTotal)
	}
}

// ── Booking: reservasi lahir atomik bersama Booking ─────────────────────────

func TestIntegration_Land_BookingReserveOnCreate(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "available")
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	svc := lbWire(db, nil, nil)

	qty := decimal.RequireFromString("100")
	b, err := svc.CreateBooking(ctx, lbTenant, lbBookingReq(unitID, customerID, &qty))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	if b.LandStockID == nil || *b.LandStockID != pool.ID {
		t.Errorf("LandStockID = %v, want %d", b.LandStockID, pool.ID)
	}
	if b.LandReservationID == nil {
		t.Fatal("LandReservationID harus terisi")
	}
	if b.LandUnitPriceSnapshot == nil || !b.LandUnitPriceSnapshot.Equal(domain.FromInt(500_000)) {
		t.Errorf("LandUnitPriceSnapshot = %v, want 500000 (dari pool)", b.LandUnitPriceSnapshot)
	}

	var resv struct {
		Status     string
		CustomerID uint64
		QuantityM2 string
	}
	db.Raw(`SELECT status, customer_id, quantity_m2 FROM land_stock_reservations WHERE id = ?`, *b.LandReservationID).Scan(&resv)
	if resv.Status != string(land.ReservationStatusActive) || resv.CustomerID != customerID || resv.QuantityM2 != "100.0000" {
		t.Errorf("reservasi = %+v, want active/%d/100.0000", resv, customerID)
	}

	var reserved string
	db.Raw(`SELECT reserved_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&reserved)
	if reserved != "100.0000" {
		t.Errorf("pool.reserved_quantity_m2 = %s, want 100.0000", reserved)
	}
}

// ── Booking: pembatalan melepas reservasi, atomik ───────────────────────────

func TestIntegration_Land_BookingCancelReleasesReservation(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "available")
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	svc := lbWire(db, nil, nil)

	qty := decimal.RequireFromString("100")
	b, err := svc.CreateBooking(ctx, lbTenant, lbBookingReq(unitID, customerID, &qty))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}

	if _, err := svc.CancelBooking(ctx, lbTenant, b.ID, "buyer mundur", time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatalf("CancelBooking: %v", err)
	}

	var status string
	db.Raw(`SELECT status FROM land_stock_reservations WHERE id = ?`, *b.LandReservationID).Scan(&status)
	if status != string(land.ReservationStatusCancelled) {
		t.Errorf("reservasi status = %s, want cancelled", status)
	}
	var reserved string
	db.Raw(`SELECT reserved_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&reserved)
	if reserved != "0.0000" {
		t.Errorf("pool.reserved_quantity_m2 pasca-batal = %s, want 0.0000 (dilepas)", reserved)
	}
}

// ── Booking: kapasitas kurang → SELURUH booking batal, bukan cuma tanahnya ──

func TestIntegration_Land_BookingCapacityExceeded_FullRollback(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "available")
	lbMustPool(t, db, projectID, "50", "500000") // hanya 50 m2 tersedia
	svc := lbWire(db, nil, nil)

	qty := decimal.RequireFromString("100") // minta lebih dari yang ada
	_, err := svc.CreateBooking(ctx, lbTenant, lbBookingReq(unitID, customerID, &qty))
	if !errors.Is(err, land.ErrCapacityExceeded) {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}

	for q, label := range map[string]string{
		"SELECT COUNT(*) FROM bookings WHERE tenant_id=?":                 "bookings",
		"SELECT COUNT(*) FROM land_stock_reservations WHERE tenant_id=?":  "land_stock_reservations",
		"SELECT COUNT(*) FROM journal_entries WHERE tenant_id=?":         "journal_entries",
		"SELECT COUNT(*) FROM termin_payments WHERE tenant_id=?":        "termin_payments",
	} {
		var n int64
		db.Raw(q, lbTenant).Scan(&n)
		if n != 0 {
			t.Errorf("%s harus 0 (rollback penuh), got %d", label, n)
		}
	}
	var status string
	db.Raw("SELECT status FROM units WHERE id = ?", unitID).Scan(&status)
	if status != "available" {
		t.Errorf("unit status = %s, want available (tak tersentuh)", status)
	}
}

// ── Kontrak langsung (tanpa Booking): reservasi juga atomik ─────────────────

func TestIntegration_Land_DirectContractReserve(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "available")
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	svc := lbWire(db, nil, nil)

	qty := decimal.RequireFromString("150")
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB Langsung",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(1_000_000_000),
		CustomerID:     &cust,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if contract.LandStockID == nil || *contract.LandStockID != pool.ID {
		t.Errorf("LandStockID = %v, want %d", contract.LandStockID, pool.ID)
	}
	if contract.LandReservationID == nil {
		t.Fatal("LandReservationID harus terisi")
	}
	var reserved string
	db.Raw(`SELECT reserved_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&reserved)
	if reserved != "150.0000" {
		t.Errorf("pool.reserved_quantity_m2 = %s, want 150.0000", reserved)
	}
}

// ── Akad terbundel: rumah + tanah diakui ATOMIK, satu transaksi, balanced ───
// Membuktikan §D3: revenue+HPP tanah adalah jurnal SENDIRI tapi terbit di
// transaksi Akad yang SAMA dengan Event3/4 unit, memakai akun piutang yang
// sama (bukan kas/bank kedua), dan reservasi tanah dikonversi (bukan hanya
// ditutup) begitu Akad rumah tercatat.
func TestIntegration_Land_BundledAkad_AtomicAndBalanced(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	// Unit langsung 'reserved' (fixture, mengikuti pola bbSeedUnit — status
	// awal yang sah untuk Akad, tanpa perlu mensimulasikan seluruh funnel).
	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	pool := lbMustPool(t, db, projectID, "1000", "500000")

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000),
		Method:    land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})

	qty := decimal.RequireFromString("100")
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB Akad",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(1_000_000_000),
		CustomerID:     &cust,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if contract.LandReservationID == nil {
		t.Fatal("LandReservationID harus terisi sebelum Akad")
	}

	journalsBefore := int64(0)
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", lbTenant).Scan(&journalsBefore)

	akadDate := time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC)
	rec, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID:    unitID,
		SalePrice: domain.FromInt(2_000_000_000),
		BuyerRef:  "Buyer LB Akad",
		BASTDate:  akadDate,
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if rec.RevenueJournalID == 0 {
		t.Fatal("rumah: RevenueJournalID harus ada")
	}
	lbJournalBalanced(t, db, rec.RevenueJournalID, "2000000000")

	// ── Jurnal tanah: DPP 100 x 500.000 = 50.000.000, HPP 100 x 200.000 = 20.000.000 ──
	var landSale struct {
		Status           string
		QuantityM2       string
		DPPAmount        string
		GrossAmount      string
		RevenueJournalID *uint64
		CogsJournalID    *uint64
		ReservationID    *uint64
	}
	if err := db.Raw(`SELECT status, quantity_m2, dpp_amount, gross_amount, revenue_journal_id, cogs_journal_id, reservation_id
		FROM land_sales WHERE tenant_id = ? AND land_stock_id = ?`, lbTenant, pool.ID).Scan(&landSale).Error; err != nil {
		t.Fatalf("baca land_sales: %v", err)
	}
	if landSale.Status != "akad" || landSale.QuantityM2 != "100.0000" {
		t.Errorf("land_sales = %+v, want akad/100.0000", landSale)
	}
	if landSale.DPPAmount != "50000000.0000" || landSale.GrossAmount != "50000000.0000" {
		t.Errorf("land_sales DPP/gross = %s/%s, want 50000000.0000/50000000.0000", landSale.DPPAmount, landSale.GrossAmount)
	}
	if landSale.RevenueJournalID == nil || landSale.CogsJournalID == nil {
		t.Fatalf("land_sales harus punya kedua jurnal, got %+v", landSale)
	}
	lbJournalBalanced(t, db, *landSale.RevenueJournalID, "50000000")
	lbJournalBalanced(t, db, *landSale.CogsJournalID, "20000000")

	// ── Atomicity §D3: SEMUA jurnal (rumah + tanah) terbit dalam SATU Akad ──
	var journalsAfter int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", lbTenant).Scan(&journalsAfter)
	// Rumah: revenue (HPP rumah nol → tanpa COGS). Tanah: revenue + COGS.
	if journalsAfter-journalsBefore != 3 {
		t.Errorf("jurnal baru = %d, want 3 (revenue rumah + revenue tanah + COGS tanah)", journalsAfter-journalsBefore)
	}

	// ── Reservasi tanah: active → converted (bukan cancelled/expired) ──────
	var resvStatus string
	db.Raw(`SELECT status FROM land_stock_reservations WHERE id = ?`, *contract.LandReservationID).Scan(&resvStatus)
	if resvStatus != string(land.ReservationStatusConverted) {
		t.Errorf("reservasi status = %s, want converted", resvStatus)
	}

	// ── Pool: reserved→sold transfer, bukan dua kali terhitung ──────────────
	var reservedAfter, soldAfter string
	db.Raw(`SELECT reserved_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&reservedAfter)
	db.Raw(`SELECT sold_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&soldAfter)
	if reservedAfter != "0.0000" {
		t.Errorf("pool.reserved_quantity_m2 pasca-Akad = %s, want 0.0000", reservedAfter)
	}
	if soldAfter != "100.0000" {
		t.Errorf("pool.sold_quantity_m2 pasca-Akad = %s, want 100.0000", soldAfter)
	}
}

// ── Konkurensi: dua kontrak berebut kapasitas yang sama, tak boleh oversell ──
// Membuktikan row-lock (SELECT ... FOR UPDATE di land.ReserveTx) benar-benar
// menyerialkan dua transaksi yang bersaing atas pool yang SAMA: total yang
// diminta (60+60=120) melebihi kapasitas (100), jadi TEPAT SATU boleh
// berhasil — bukan keduanya (oversell) dan bukan keduanya gagal (deadlock).
func TestIntegration_Land_ConcurrentReserve_NoOversell(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitA, customerID := lbSeed(t, db, "available")
	var unitB uint64
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`, lbTenant, projectID, "LB-02", "villa", domain.FromInt(100), domain.FromInt(0), "available").Error; err != nil {
		t.Fatalf("seed unit B: %v", err)
	}
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitB)

	pool := lbMustPool(t, db, projectID, "100", "500000") // kapasitas 100 m2, TIDAK cukup untuk 60+60
	svc := lbWire(db, nil, nil)

	run := func(unitID uint64) (*sale.SaleContract, error) {
		qty := decimal.RequireFromString("60")
		cust := customerID
		return svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
			UnitID:         unitID,
			BuyerName:      "Buyer LB Concurrent",
			PaymentType:    sale.PaymentTypeTunai,
			ContractDate:   time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
			TotalPrice:     domain.FromInt(1_000_000_000),
			CustomerID:     &cust,
			LandQuantityM2: &qty,
		})
	}

	var wg sync.WaitGroup
	results := make([]error, 2)
	units := []uint64{unitA, unitB}
	wg.Add(2)
	for i := 0; i < 2; i++ {
		go func(i int) {
			defer wg.Done()
			_, err := run(units[i])
			results[i] = err
		}(i)
	}
	wg.Wait()

	oks, capExceeded := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			oks++
		case errors.Is(err, land.ErrCapacityExceeded):
			capExceeded++
		default:
			t.Fatalf("error tak terduga: %v", err)
		}
	}
	if oks != 1 || capExceeded != 1 {
		t.Fatalf("hasil = %d ok / %d capacity-exceeded, want tepat 1/1 (no oversell, no deadlock)", oks, capExceeded)
	}

	var reserved string
	db.Raw(`SELECT reserved_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&reserved)
	if reserved != "60.0000" {
		t.Errorf("pool.reserved_quantity_m2 = %s, want 60.0000 (hanya satu reservasi masuk)", reserved)
	}
	var resvCount int64
	db.Raw(`SELECT COUNT(*) FROM land_stock_reservations WHERE tenant_id = ?`, lbTenant).Scan(&resvCount)
	if resvCount != 1 {
		t.Errorf("jumlah reservasi = %d, want 1", resvCount)
	}
}
