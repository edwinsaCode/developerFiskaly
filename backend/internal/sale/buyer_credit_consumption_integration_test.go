//go:build integration

package sale_test

// Bug fix Saldo Kredit Buyer (2026-09-04) — integration (real MySQL).
//
// Kasus nyata kontrak #4407: GetBuyerCredit lama hanya mengurangi
// buyer_credit dengan credit_applications EKSPLISIT (ApplyCredit) — konsumsi
// IMPLISIT saat Akad (netting Uang Muka Penjualan Event 3, buildEvent3Lines)
// tidak pernah tercatat, sehingga saldo yang sudah habis dipakai tetap
// terlihat "tersedia" selamanya (Rp37.000.000 pada #4407).
//
// Perbaikan: consumeRemainingCreditInTx menjadikan credit_applications SATU
// source of truth untuk SELURUH konsumsi (eksplisit via ApplyCredit MAUPUN
// otomatis saat Akad/pembatalan), dipanggil atomik di dalam tx yang sama.
//
// Test ini membuktikan siklus hidup penuh: tersedia → dikonsumsi otomatis
// saat Akad → tidak bisa dipakai dua kali (baik lewat Akad ulang/idempoten
// maupun ApplyCredit eksplisit) → tetap benar meski dibundel dengan Kelebihan
// Tanah (tidak double count).
//
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (≥ 000097).

import (
	"context"
	"errors"
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

// bcRepo membangun *sale.GORMRepository murni untuk query saldo kredit
// (GetBuyerCredit) — wiring identik lbWire/itNewRepo, dipisah agar test bisa
// membaca state lewat repo tanpa harus melewati Service.GetBuyerCredit (yang
// menuntut contractID, bukan unitID).
func bcRepo(db *gorm.DB) *sale.GORMRepository {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	return sale.NewGORMRepository(db, posting, allocSvc)
}

// bcSeedBuyerCredit menyeed saldo kredit `amount` untuk `unitID` di lbTenant
// (1 termin + 1 baris payment_allocations buyer_credit) — pola identik
// ccSeedBuyerCredit (credit_application_integration_test.go), diparameterkan
// ke lbTenant supaya bisa dipakai berdampingan dengan fixture project/unit
// land_integration_test.go (lbSeed/lbWire/lbMustPool).
func bcSeedBuyerCredit(t *testing.T, db *gorm.DB, unitID uint64, amount int64) uint64 {
	t.Helper()
	tp := &sale.TerminPayment{
		TenantID: lbTenant, UnitID: unitID, ProjectID: 10, Amount: domain.FromInt(amount),
		BankAccountCode: "1-1300", Date: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC),
		JournalEntryID: 1, CreditAccountCode: "2-2000",
	}
	if err := db.Create(tp).Error; err != nil {
		t.Fatalf("seed termin buyer credit: %v", err)
	}
	if err := db.Exec(`INSERT INTO payment_allocations (tenant_id, termin_payment_id, allocation_type, amount)
		VALUES (?,?,?,?)`, lbTenant, tp.ID, "buyer_credit", domain.FromInt(amount)).Error; err != nil {
		t.Fatalf("seed buyer_credit: %v", err)
	}
	return tp.ID
}

// ── Kredit tersedia sebelum digunakan (baseline, tanpa konsumsi apa pun) ────
func TestIntegration_BuyerCredit_AvailableBeforeUse(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	_, unitID, _ := lbSeed(t, db, "reserved")
	repo := bcRepo(db)

	bcSeedBuyerCredit(t, db, unitID, 15_000_000)

	view, err := repo.GetBuyerCredit(ctx, lbTenant, unitID)
	if err != nil {
		t.Fatalf("GetBuyerCredit: %v", err)
	}
	if view.Available != "15000000" || view.TotalSources != "15000000" || view.TotalApplied != "0" {
		t.Errorf("saldo baseline salah: %+v", view)
	}
}

// ── Kasus nyata #4407: kredit tersedia → dikonsumsi OTOMATIS saat Akad ──────
func TestIntegration_BuyerCredit_ConsumedAutomaticallyAtAkad(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	_, unitID, customerID := lbSeed(t, db, "reserved")
	svc := lbWire(db, nil, lbZeroUnitHPPResolver{})
	repo := bcRepo(db)

	cust := customerID
	contract, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "Buyer LB Kredit", PaymentType: sale.PaymentTypeTunai,
		ContractDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:   domain.FromInt(2_000_000_000), CustomerID: &cust,
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	// Reproduksi #4407: buyer sudah punya saldo kredit (kelebihan bayar
	// pra-Akad) Rp37jt, BELUM pernah dipakai ke cicilan mana pun.
	bcSeedBuyerCredit(t, db, unitID, 37_000_000)

	// ── 1. Kredit TERSEDIA sebelum digunakan ────────────────────────────────
	before, err := repo.GetBuyerCredit(ctx, lbTenant, unitID)
	if err != nil {
		t.Fatalf("GetBuyerCredit (sebelum Akad): %v", err)
	}
	if before.Available != "37000000" {
		t.Fatalf("available sebelum Akad = %s, want 37000000", before.Available)
	}

	rec, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(2_000_000_000), BuyerRef: "Buyer LB Kredit",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}
	lbJournalBalanced(t, db, rec.RevenueJournalID, "2000000000")

	// ── 2. BUG FIX #4407: pasca-Akad, saldo yang dikonsumsi netting Uang Muka
	//    TIDAK BOLEH lagi terlihat tersedia ──────────────────────────────────
	after, err := repo.GetBuyerCredit(ctx, lbTenant, unitID)
	if err != nil {
		t.Fatalf("GetBuyerCredit (setelah Akad): %v", err)
	}
	if after.Available != "0" {
		t.Fatalf("BUG #4407 BELUM FIX: available setelah Akad = %s, want 0 (kredit sudah dikonsumsi netting Uang Muka)", after.Available)
	}
	if after.TotalApplied != "37000000" {
		t.Fatalf("total_applied setelah Akad = %s, want 37000000", after.TotalApplied)
	}

	// ── 3. credit_applications = SATU sumber kebenaran: SATU baris baru,
	//    payment_schedule_id NULL (konsumsi otomatis), sale_contract_id FK
	//    audit ke kontrak yang baru di-Akad-kan ─────────────────────────────
	var ca struct {
		SaleContractID    *uint64
		UnitID            uint64
		Amount            string
		PaymentScheduleID *uint64
	}
	if err := db.Raw(`SELECT sale_contract_id, unit_id, amount, payment_schedule_id
		FROM credit_applications WHERE tenant_id = ? AND unit_id = ?`, lbTenant, unitID).Scan(&ca).Error; err != nil {
		t.Fatalf("baca credit_applications: %v", err)
	}
	if ca.SaleContractID == nil || *ca.SaleContractID != contract.ID {
		t.Errorf("credit_applications.sale_contract_id = %v, want %d", ca.SaleContractID, contract.ID)
	}
	if ca.Amount != "37000000.0000" || ca.PaymentScheduleID != nil {
		t.Errorf("credit_applications = %+v, want amount=37000000.0000 schedule=nil", ca)
	}
	var n int64
	db.Raw(`SELECT COUNT(*) FROM credit_applications WHERE tenant_id = ? AND unit_id = ?`, lbTenant, unitID).Scan(&n)
	if n != 1 {
		t.Errorf("jumlah credit_applications = %d, want 1 (bukan double count)", n)
	}

	// ── 4. Kredit TIDAK BISA dipakai dua kali (jalur otomatis): pemanggilan
	//    ulang konsumsi pada state yang sama adalah NO-OP idempoten ─────────
	if err := svc.ConsumeRemainingCreditInTx(ctx, db, lbTenant, unitID, contract.ID, "percobaan konsumsi ganda", nil); err != nil {
		t.Fatalf("ConsumeRemainingCreditInTx (harus idempoten no-op): %v", err)
	}
	db.Raw(`SELECT COUNT(*) FROM credit_applications WHERE tenant_id = ? AND unit_id = ?`, lbTenant, unitID).Scan(&n)
	if n != 1 {
		t.Errorf("jumlah credit_applications setelah percobaan konsumsi ganda = %d, want tetap 1 (idempoten)", n)
	}

	// ── 5. Kredit TIDAK BISA dipakai dua kali (jalur eksplisit): available
	//    sudah 0 → ApplyCredit ke cicilan manapun harus ditolak ─────────────
	// Kontrak tunai tidak otomatis punya payment_schedules (tanpa cicilan) —
	// seed satu baris manual (pola bfSeedScheduleRow) semata untuk membuktikan
	// ApplyCredit ditolak sekalipun ada cicilan valid untuk dituju.
	sched := &sale.PaymentSchedule{
		TenantID: lbTenant, SaleContractID: contract.ID, UnitID: unitID, InstallmentNumber: 1,
		DueDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(10_000_000), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled,
	}
	if err := db.Create(sched).Error; err != nil {
		t.Fatalf("seed payment_schedule: %v", err)
	}
	if _, err := svc.ApplyCredit(ctx, lbTenant, contract.ID, sale.ApplyCreditRequest{ScheduleID: sched.ID}); !errors.Is(err, sale.ErrNoCreditAvailable) {
		t.Errorf("ApplyCredit pasca-konsumsi-otomatis = %v, want ErrNoCreditAvailable", err)
	}
}

// ── Kredit + Kelebihan Tanah bundled: konsumsi tetap tunggal, tak double count ──
func TestIntegration_BuyerCredit_ConsumedAtBundledLandAkad(t *testing.T) {
	db := itConnect(t)
	lbCleanup(t, db)
	defer lbCleanup(t, db)
	ctx := context.Background()

	projectID, unitID, customerID := lbSeed(t, db, "reserved")
	lbMustPool(t, db, projectID, "1000", "500000")

	hppStub := &lbStubHPPResolver{res: land.LandHPPResolution{
		RatePerM2: domain.FromInt(200_000), Method: land.HPPMethodActual,
	}}
	svc := lbWire(db, hppStub, lbZeroUnitHPPResolver{})
	repo := bcRepo(db)

	const housePrice = 185_000_000
	qty := decimal.RequireFromString("50")
	cust := customerID
	if _, err := svc.CreateContract(ctx, lbTenant, sale.CreateContractRequest{
		UnitID: unitID, BuyerName: "Buyer LB Kredit+Tanah", PaymentType: sale.PaymentTypeTunai,
		ContractDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
		TotalPrice:   domain.FromInt(housePrice), CustomerID: &cust, LandQuantityM2: &qty,
	}); err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	bcSeedBuyerCredit(t, db, unitID, 10_000_000) // saldo kredit sebelum Akad

	if _, err := svc.RecordAkad(ctx, lbTenant, sale.RecordBASTRequest{
		UnitID: unitID, SalePrice: domain.FromInt(housePrice), BuyerRef: "Buyer LB Kredit+Tanah",
		BASTDate: time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	after, err := repo.GetBuyerCredit(ctx, lbTenant, unitID)
	if err != nil {
		t.Fatalf("GetBuyerCredit: %v", err)
	}
	if after.Available != "0" {
		t.Errorf("available pasca-Akad bundled tanah = %s, want 0", after.Available)
	}
	var n int64
	db.Raw(`SELECT COUNT(*) FROM credit_applications WHERE tenant_id = ? AND unit_id = ?`, lbTenant, unitID).Scan(&n)
	if n != 1 {
		t.Errorf("jumlah credit_applications = %d, want 1 (tidak double count meski ada komponen tanah)", n)
	}
}
