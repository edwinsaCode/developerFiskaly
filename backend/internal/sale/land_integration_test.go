//go:build integration

package sale_test

// kelebihan-tanah-konversi-kontrak-2026-08 — integration (real MySQL):
// membuktikan komponen Produk Tambahan Kelebihan Tanah yang menempel pada
// Kontrak/Akad unit benar-benar atomik dengan siklus hidup finansialnya
// sendiri. Booking TIDAK LAGI membawa komponen tanah — dipilih di Konversi
// Kontrak (baik kontrak langsung maupun konversi dari Booking), bukan di
// Booking. Kelebihan kapasitas membatalkan SELURUH konversi/kontrak
// (rollback penuh, bukan cuma komponen tanahnya), dan Akad membukukan
// jurnal pendapatan+HPP rumah DAN tanah dalam SATU transaksi (§D3), balanced.
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (≥ 000089/000090).

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
	opts := []sale.ServiceOption{sale.WithContractStore(repo), sale.WithBookingStore(repo), sale.WithPaymentCommitter(repo)}
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

// lbBookingReq: Booking TIDAK LAGI membawa komponen Kelebihan Tanah
// (kelebihan-tanah-konversi-kontrak-2026-08) — dipilih di Konversi Kontrak.
func lbBookingReq(unitID, customerID uint64) sale.CreateBookingRequest {
	return sale.CreateBookingRequest{
		UnitID:          unitID,
		CustomerID:      customerID,
		BookingFee:      domain.FromInt(5_000_000),
		Refundable:      false,
		BankAccountCode: "1-1300",
		BookingDate:     time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		ExpiryDate:      time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC),
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

// ── Booking: TIDAK menyentuh tanah sama sekali ──────────────────────────────
// kelebihan-tanah-konversi-kontrak-2026-08: Booking tak lagi punya field
// land_* (migration 000089) — CreateBooking tak pernah membuat reservasi.

func TestIntegration_Land_BookingNeverTouchesLand(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "available")
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	svc := lbWire(db, nil, nil)

	if _, err := svc.CreateBooking(ctx, lbTenant, lbBookingReq(unitID, customerID)); err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}

	var resvCount int64
	db.Raw(`SELECT COUNT(*) FROM land_stock_reservations WHERE tenant_id = ?`, lbTenant).Scan(&resvCount)
	if resvCount != 0 {
		t.Errorf("land_stock_reservations = %d, want 0 (Booking tak boleh menyentuh tanah)", resvCount)
	}
	var reserved string
	db.Raw(`SELECT reserved_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&reserved)
	if reserved != "0.0000" {
		t.Errorf("pool.reserved_quantity_m2 = %s, want 0.0000", reserved)
	}
}

// ── Konversi Kontrak: reservasi lahir atomik saat Booking dikonversi ───────
// kelebihan-tanah-konversi-kontrak-2026-08: komponen tanah dipilih di
// Konversi Kontrak, bukan di Booking — ConvertWithContractAtomic mereservasi
// dengan pola identik SaveContract (booking_repo.go).

func TestIntegration_Land_ConversionReserveOnCreate(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "available")
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	svc := lbWire(db, nil, nil)

	b, err := svc.CreateBooking(ctx, lbTenant, lbBookingReq(unitID, customerID))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}

	qty := decimal.RequireFromString("100")
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB Konversi",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(1_000_000_000),
		CustomerID:     &cust,
		BookingID:      &b.ID,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract (konversi): %v", err)
	}
	if contract.LandStockID == nil || *contract.LandStockID != pool.ID {
		t.Errorf("LandStockID = %v, want %d", contract.LandStockID, pool.ID)
	}
	if contract.LandReservationID == nil {
		t.Fatal("LandReservationID harus terisi")
	}
	if contract.LandUnitPriceSnapshot == nil || !contract.LandUnitPriceSnapshot.Equal(domain.FromInt(500_000)) {
		t.Errorf("LandUnitPriceSnapshot = %v, want 500000 (dari pool)", contract.LandUnitPriceSnapshot)
	}

	var resv struct {
		Status     string
		CustomerID uint64
		QuantityM2 string
	}
	db.Raw(`SELECT status, customer_id, quantity_m2 FROM land_stock_reservations WHERE id = ?`, *contract.LandReservationID).Scan(&resv)
	if resv.Status != string(land.ReservationStatusActive) || resv.CustomerID != customerID || resv.QuantityM2 != "100.0000" {
		t.Errorf("reservasi = %+v, want active/%d/100.0000", resv, customerID)
	}

	var reserved string
	db.Raw(`SELECT reserved_quantity_m2 FROM land_stock WHERE id = ?`, pool.ID).Scan(&reserved)
	if reserved != "100.0000" {
		t.Errorf("pool.reserved_quantity_m2 = %s, want 100.0000", reserved)
	}
}

// ── Konversi Kontrak: kapasitas kurang → SELURUH konversi batal ────────────
// Booking tetap 'active' & unit tetap 'booked' — tak ada rollback parsial.

func TestIntegration_Land_ConversionCapacityExceeded_FullRollback(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "available")
	lbMustPool(t, db, projectID, "50", "500000") // hanya 50 m2 tersedia
	svc := lbWire(db, nil, nil)

	b, err := svc.CreateBooking(ctx, lbTenant, lbBookingReq(unitID, customerID))
	if err != nil {
		t.Fatalf("CreateBooking: %v", err)
	}
	// CreateBooking sah membukukan jurnal Titipan Booking sendiri — baseline
	// diambil SETELAH booking, agar assert di bawah murni membuktikan konversi
	// yang gagal tidak menambah apa pun (bukan bahwa booking tanpa jurnal).
	var journalsBefore int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", lbTenant).Scan(&journalsBefore)

	qty := decimal.RequireFromString("100") // minta lebih dari yang ada
	cust := customerID
	_, err = svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB Konversi Gagal",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(1_000_000_000),
		CustomerID:     &cust,
		BookingID:      &b.ID,
		LandQuantityM2: &qty,
	})
	if !errors.Is(err, land.ErrCapacityExceeded) {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}

	for q, label := range map[string]string{
		"SELECT COUNT(*) FROM sale_contracts WHERE tenant_id=?":          "sale_contracts",
		"SELECT COUNT(*) FROM land_stock_reservations WHERE tenant_id=?": "land_stock_reservations",
	} {
		var n int64
		db.Raw(q, lbTenant).Scan(&n)
		if n != 0 {
			t.Errorf("%s harus 0 (rollback penuh), got %d", label, n)
		}
	}
	var journalsAfter int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", lbTenant).Scan(&journalsAfter)
	if journalsAfter != journalsBefore {
		t.Errorf("journal_entries bertambah dari %d ke %d, want tetap (konversi gagal harus rollback penuh)", journalsBefore, journalsAfter)
	}
	var bookingStatus string
	db.Raw("SELECT status FROM bookings WHERE id = ?", b.ID).Scan(&bookingStatus)
	if bookingStatus != string(sale.BookingStatusActive) {
		t.Errorf("booking status = %s, want active (konversi gagal, booking tetap aktif)", bookingStatus)
	}
	var unitStatus string
	db.Raw("SELECT status FROM units WHERE id = ?", unitID).Scan(&unitStatus)
	if unitStatus != "booked" {
		t.Errorf("unit status = %s, want booked (tak berubah oleh konversi yang gagal)", unitStatus)
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

// ── Bug UAT 2026-09: Penerimaan mengabaikan piutang Kelebihan Tanah ────────
//
// Sebelum fix (migrasi 000095 + payment_schedules land row + guard/waterfall
// aditif): kontrak Unit + Kelebihan Tanah menghasilkan Piutang Usaha gabungan
// yang BENAR di Neraca (unit + land, sama-sama ke 1-2000 pada kontrak Tunai),
// tapi menu Penerimaan (outstanding/max payment) hanya menghitung piutang
// unit — piutang tanah tidak pernah muncul sebagai piutang yang bisa
// dialokasikan, sehingga customer TIDAK BISA melunasi seluruh piutangnya.
//
// Test ini membuktikan, dalam SATU alur end-to-end:
//  1. Akad (rumah+tanah bundled) menghasilkan AR gabungan = harga unit + gross
//     tanah (bukan cuma unit).
//  2. Pembayaran PARSIAL (kurang dari harga rumah) dialokasikan waterfall ke
//     rumah dulu (cicilan presedensi) — tanah tetap outstanding.
//  3. Pembayaran PELUNASAN (sisa penuh, rumah+tanah) diterima TANPA ditolak
//     guard overpayment (bug lama: guard cuma tahu piutang rumah) — dan
//     KEDUA baris jadwal (rumah, tanah) menjadi lunas.
//  4. Saldo akun Piutang Usaha (1-2000) atas unit ini kembali 0 (Neraca
//     reconcile — jurnal Dr Kas/Bank / Cr Piutang benar).
//  5. Overpayment (bayar lagi walau outstanding 0) tetap DITOLAK.
//  6. Retry dengan idempotency key yang sama TIDAK membuat termin/jurnal
//     kedua (aman di-retry).
func TestIntegration_Land_ReceivePayment_CoversUnitAndLandAR(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	// Harga jual tanah 500.000/m² x 50 m² = 25.000.000 (RpX pada skenario klien).
	pool := lbMustPool(t, db, projectID, "1000", "500000")
	_ = pool

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000),
		Method:    land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})

	const housePrice = 185_000_000 // Rp185jt — persis skenario klien.
	const landQtyM2 = "50"
	const landGross = 25_000_000 // 50 x 500.000 — RpX.
	const combinedAR = housePrice + landGross

	qty := decimal.RequireFromString(landQtyM2)
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB AR",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(housePrice),
		CustomerID:     &cust,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	rec, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(housePrice), BuyerRef: "Buyer LB AR",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	if rec.RevenueJournalID == 0 {
		t.Fatal("rumah: RevenueJournalID harus ada")
	}

	// ── AR gabungan tercatat sebagai DUA baris jadwal: rumah (fallback lump-sum,
	//    lihat repository.go Execute) + tanah (ScheduleTypeLand, migrasi 000095) ──
	type schedRow struct {
		Type        string
		Amount      string
		PaidAmount  string
		Status      string
		Installment int `gorm:"column:installment_number"`
	}
	loadSchedules := func() []schedRow {
		var rows []schedRow
		if err := db.Table("payment_schedules").
			Select("type, amount, paid_amount, status, installment_number").
			Where("tenant_id = ? AND sale_contract_id = ?", lbTenant, contract.ID).
			Order("installment_number ASC").
			Scan(&rows).Error; err != nil {
			t.Fatalf("baca payment_schedules: %v", err)
		}
		return rows
	}
	schedules := loadSchedules()
	if len(schedules) != 2 {
		t.Fatalf("jumlah baris payment_schedules pasca-Akad = %d, want 2 (rumah+tanah), got %+v", len(schedules), schedules)
	}
	var houseAmt, landAmt string
	for _, r := range schedules {
		switch r.Type {
		case "land":
			landAmt = r.Amount
		default:
			houseAmt = r.Amount
		}
	}
	if houseAmt != "185000000.0000" {
		t.Errorf("baris jadwal rumah = %s, want 185000000.0000", houseAmt)
	}
	if landAmt != "25000000.0000" {
		t.Errorf("baris jadwal tanah = %s, want 25000000.0000", landAmt)
	}

	// ── 1-2000 pasca-Akad: gabungan rumah+tanah (Neraca sudah benar SEBELUM fix) ──
	// Tidak difilter per unit_id: jurnal AR tanah (internal/land/akad.go) berdiri
	// sendiri dari sale_contracts.unit_id (§B.3) dan tidak membawa unit_id pada
	// journal_lines-nya — saldo Neraca akun 1-2000 selalu dihitung tenant-wide.
	// Fixture ini hanya punya satu kontrak per tenant sehingga scoping tenant
	// setara dengan scoping per-kontrak untuk keperluan assert reconcile.
	unit2000Balance := func() string {
		var v string
		if err := db.Raw(`SELECT COALESCE(SUM(jl.debit - jl.credit), 0) FROM journal_lines jl
			JOIN accounts a ON a.id = jl.account_id
			JOIN journal_entries je ON je.id = jl.journal_entry_id
			WHERE jl.tenant_id = ? AND a.code = ? AND je.posted_at IS NOT NULL`,
			lbTenant, "1-2000").Scan(&v).Error; err != nil {
			t.Fatalf("query saldo 1-2000: %v", err)
		}
		m, _ := domain.NewMoney(v)
		return m.String()
	}
	if got := unit2000Balance(); got != "210000000" {
		t.Fatalf("saldo 1-2000 pasca-Akad = %s, want 210000000 (185jt rumah + 25jt tanah)", got)
	}

	// ── 2. Pembayaran PARSIAL — kurang dari harga rumah, tanah harus TETAP outstanding ──
	cid := contract.ID
	partial, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(100_000_000), Date: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lb-ar-partial-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (parsial): %v", err)
	}
	if partial.RemainingBalance != "110000000" {
		t.Errorf("RemainingBalance pasca-parsial = %s, want 110000000 (85jt rumah + 25jt tanah)", partial.RemainingBalance)
	}
	schedules = loadSchedules()
	for _, r := range schedules {
		if r.Type == "land" && r.PaidAmount != "0.0000" {
			t.Errorf("BUG: baris tanah ikut terbayar oleh pembayaran parsial (waterfall rumah harus presedensi): paid_amount=%s", r.PaidAmount)
		}
		if r.Type != "land" && r.PaidAmount != "100000000.0000" {
			t.Errorf("baris rumah paid_amount = %s, want 100000000.0000", r.PaidAmount)
		}
	}

	// ── 3. Pelunasan PENUH — sisa 110jt (85jt rumah + 25jt tanah) — TIDAK BOLEH
	//    ditolak guard overpayment (bug lama: guard hanya tahu piutang rumah,
	//    85jt < 110jt akan salah menolak sebelum fix) ──
	full, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(110_000_000), Date: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lb-ar-full-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (pelunasan penuh, seharusnya TIDAK ditolak): %v", err)
	}
	if full.RemainingBalance != "0" {
		t.Errorf("RemainingBalance pasca-lunas = %s, want 0", full.RemainingBalance)
	}

	// ── 4. KEDUA baris jadwal lunas ──────────────────────────────────────────
	schedules = loadSchedules()
	for _, r := range schedules {
		wantPaid := "185000000.0000"
		if r.Type == "land" {
			wantPaid = "25000000.0000"
		}
		if r.PaidAmount != wantPaid {
			t.Errorf("baris %s paid_amount = %s, want %s", r.Type, r.PaidAmount, wantPaid)
		}
		if r.Status != "received" {
			t.Errorf("baris %s status = %s, want received", r.Type, r.Status)
		}
	}

	// ── Neraca reconcile: 1-2000 kembali 0 ───────────────────────────────────
	if got := unit2000Balance(); got != "0" {
		t.Errorf("saldo 1-2000 pasca-lunas = %s, want 0 (Neraca harus reconcile)", got)
	}

	// ── 5. Overpayment tetap ditolak (outstanding sudah 0) ───────────────────
	_, err = svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(1), Date: time.Date(2026, 9, 11, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lb-ar-overpay-1",
	})
	if !errors.Is(err, sale.ErrPaymentExceedsReceivable) {
		t.Errorf("overpayment error = %v, want ErrPaymentExceedsReceivable", err)
	}

	// ── 6. Retry idempoten atas pembayaran pelunasan — TIDAK membuat termin/
	//    jurnal kedua, hasil identik ──────────────────────────────────────────
	retry, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(110_000_000), Date: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lb-ar-full-1",
	})
	if err != nil {
		t.Fatalf("ReceivePayment (retry idempotency): %v", err)
	}
	if retry.TerminID != full.TerminID {
		t.Errorf("retry idempotency menghasilkan termin BARU: got %d, want %d (sama dgn payment asli)", retry.TerminID, full.TerminID)
	}
	if got := unit2000Balance(); got != "0" {
		t.Errorf("saldo 1-2000 pasca-retry = %s, want tetap 0 (retry tak boleh posting jurnal kedua)", got)
	}
	var terminCount int64
	db.Raw(`SELECT COUNT(*) FROM termin_payments WHERE tenant_id = ? AND unit_id = ?`, lbTenant, unitID).Scan(&terminCount)
	if terminCount != 2 {
		t.Errorf("jumlah termin_payments = %d, want 2 (parsial + lunas — retry TIDAK menambah baris)", terminCount)
	}

	_ = combinedAR // dipakai sebagai dokumentasi angka skenario (185jt+25jt=210jt)
}

// TestIntegration_Land_CancelPaidBundledLandSale_RejectsStandaloneCancel
// membuktikan guard baru (land.ErrLandSaleHasReceivedPayment): land_sale
// BUNDLED yang piutangnya (payment_schedules.land_sale_id) sudah menerima
// pembayaran TIDAK BOLEH dibatalkan lewat land.Service.CancelLandSale
// (endpoint langsung) — jalur itu tidak punya mekanisme refund/settlement,
// jadi kalau dibiarkan lanjut, uang yang sudah diterima buyer jadi kas tak
// bertuan. Pembatalan yang benar untuk skenario ini adalah lewat
// internal/cancellation (pembatalan unit), yang menghitung refund utuh —
// dibuktikan terpisah oleh
// cancellation_test.TestIntegration_Cancellation_PostAkad_WithLandComponent.
func TestIntegration_Land_CancelPaidBundledLandSale_RejectsStandaloneCancel(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	lbMustPool(t, db, projectID, "1000", "500000")

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000),
		Method:    land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})

	const housePrice = 185_000_000
	qty := decimal.RequireFromString("50")
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB Cancel",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(housePrice),
		CustomerID:     &cust,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if _, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(housePrice), BuyerRef: "Buyer LB Cancel",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	var landSaleID uint64
	if err := db.Raw(`SELECT id FROM land_sales WHERE tenant_id = ? AND reservation_id = ?`,
		lbTenant, *contract.LandReservationID).Scan(&landSaleID).Error; err != nil || landSaleID == 0 {
		t.Fatalf("baca land_sales.id: %v (id=%d)", err, landSaleID)
	}

	// Lunasi SELURUH AR gabungan (rumah 185jt + tanah 25jt = 210jt) — baris
	// jadwal tanah ikut jadi 'received' dengan paid_amount > 0.
	cid := contract.ID
	if _, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(210_000_000), Date: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lb-cancel-guard-full",
	}); err != nil {
		t.Fatalf("ReceivePayment (pelunasan penuh): %v", err)
	}

	landSvc := land.NewService(land.NewGORMRepository(db), land.WithHPPResolver(hppStub))
	_, err = landSvc.CancelLandSale(ctx, lbTenant, landSaleID, land.CancelLandSaleRequest{
		Reason: "percobaan batalkan langsung — harus ditolak", CancelDate: time.Now(),
	})
	if !errors.Is(err, land.ErrLandSaleHasReceivedPayment) {
		t.Fatalf("CancelLandSale (standalone, land_sale sudah dibayar) error = %v, want ErrLandSaleHasReceivedPayment", err)
	}

	// land_sale HARUS tetap akad (tidak ada perubahan) — guard menolak SEBELUM
	// jurnal apa pun dibalik.
	var statusAfter string
	if err := db.Raw(`SELECT status FROM land_sales WHERE id = ?`, landSaleID).Scan(&statusAfter).Error; err != nil {
		t.Fatalf("baca status land_sales: %v", err)
	}
	if statusAfter != "akad" {
		t.Errorf("land_sales.status pasca-percobaan-tolak = %s, want tetap akad", statusAfter)
	}
}

// TestIntegration_Land_CrossoverPayment_NoDoubleCounting membuktikan
// perbaikan bug ContractFinancialSummary.TotalOutstandingActual dan
// CustomerStatement.RemainingBalance/Exposure: formula naif yang menjumlah
// dua sisa yang MASING-MASING sudah dikurangi dari kolam pembayaran
// gabungan (rumah+tanah) akan menghitung ganda begitu waterfall meluber
// dari cicilan rumah ke baris tanah. Skenario: rumah 185jt + tanah 25jt =
// 210jt AR; bayar 200jt sekali jalan → rumah LUNAS (185jt) dan tanah
// terbayar sebagian (15jt dari 25jt) → sisa yang benar = 10jt (bukan
// negatif/salah seperti formula lama).
func TestIntegration_Land_CrossoverPayment_NoDoubleCounting(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	lbMustPool(t, db, projectID, "1000", "500000")

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000),
		Method:    land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})

	const housePrice = 185_000_000 // Rp185jt
	const landGross = 25_000_000   // 50 x 500.000
	const crossoverPayment = 200_000_000
	const wantRemaining = "10000000" // (185+25) - 200

	qty := decimal.RequireFromString("50")
	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID:         unitID,
		BuyerName:      "Buyer LB Crossover",
		PaymentType:    sale.PaymentTypeTunai,
		ContractDate:   time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:     domain.FromInt(housePrice),
		CustomerID:     &cust,
		LandQuantityM2: &qty,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}
	if _, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(housePrice), BuyerRef: "Buyer LB Crossover",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	cid := contract.ID
	if _, err := svc.ReceivePayment(ctx, lbTenant, sale.ReceivePaymentRequest{
		Source: sale.PaymentSourceCollection, ContractID: &cid,
		Amount: domain.FromInt(crossoverPayment), Date: time.Date(2026, 9, 5, 0, 0, 0, 0, time.UTC),
		BankAccountCode: "1-1300", IdempotencyKey: "lb-crossover-1",
	}); err != nil {
		t.Fatalf("ReceivePayment (crossover 200jt): %v", err)
	}

	// ── ContractFinancialSummary.TotalOutstandingActual — HARUS 10jt, bukan
	//    negatif (bug lama: Outstanding(rumah, sudah minus kolam gabungan)
	//    + landOutstanding(tanah, minus kolam gabungan lagi) = double-count) ──
	summary, err := svc.ContractFinancialSummaryByID(ctx, lbTenant, cid)
	if err != nil {
		t.Fatalf("ContractFinancialSummaryByID: %v", err)
	}
	if got := summary.TotalOutstandingActual.String(); got != wantRemaining {
		t.Errorf("TotalOutstandingActual = %s, want %s (rumah lunas 185jt + tanah tersisa 15jt dari 25jt = 10jt)", got, wantRemaining)
	}
	if summary.TotalOutstandingActual.IsNeg() {
		t.Fatalf("TotalOutstandingActual NEGATIF (%s) — bug double-counting waterfall rumah→tanah belum diperbaiki", summary.TotalOutstandingActual.String())
	}

	// ── CustomerStatement.RemainingBalance — SATU rumus dengan Summary di atas ──
	stmt, err := svc.GetCustomerStatement(ctx, lbTenant, cid, time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("GetCustomerStatement: %v", err)
	}
	if stmt.RemainingBalance != wantRemaining {
		t.Errorf("RemainingBalance = %s, want %s", stmt.RemainingBalance, wantRemaining)
	}
	if stmt.Exposure == nil {
		t.Fatal("Exposure nil")
	}
	if stmt.Exposure.HouseOutstanding != "0" {
		t.Errorf("Exposure.HouseOutstanding = %s, want 0 (rumah sudah lunas oleh crossover)", stmt.Exposure.HouseOutstanding)
	}
	if stmt.Exposure.LandOutstanding != wantRemaining {
		t.Errorf("Exposure.LandOutstanding = %s, want %s (sisa 10jt dari 25jt tanah)", stmt.Exposure.LandOutstanding, wantRemaining)
	}
	if stmt.Exposure.TotalOutstanding != wantRemaining {
		t.Errorf("Exposure.TotalOutstanding = %s, want %s", stmt.Exposure.TotalOutstanding, wantRemaining)
	}

	_ = landGross // dokumentasi angka skenario
}
