//go:build integration

package sale_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// FE-3 · P2 — integration test apply-credit (real MySQL, tx + lock).

// ccSeedBuyerCredit menyeed saldo kredit `amount` untuk `unitID` (1 termin + 1
// baris payment_allocations buyer_credit). Mengembalikan termin id.
func ccSeedBuyerCredit(t *testing.T, db *gorm.DB, unitID uint64, amount int64) uint64 {
	t.Helper()
	tp := &sale.TerminPayment{
		TenantID: itTenant, UnitID: unitID, ProjectID: 10, Amount: domain.FromInt(amount),
		BankAccountCode: "1-1300", Date: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC),
		JournalEntryID: 1, CreditAccountCode: "2-2000",
	}
	if err := db.Create(tp).Error; err != nil {
		t.Fatalf("seed termin: %v", err)
	}
	if err := db.Exec(`INSERT INTO payment_allocations (tenant_id, termin_payment_id, allocation_type, amount)
		VALUES (?,?,?,?)`, itTenant, tp.ID, "buyer_credit", domain.FromInt(amount)).Error; err != nil {
		t.Fatalf("seed buyer_credit: %v", err)
	}
	return tp.ID
}

func ccScheduleID(t *testing.T, db *gorm.DB, unitID uint64) uint64 {
	t.Helper()
	var id uint64
	if err := db.Table("payment_schedules").Select("id").
		Where("tenant_id = ? AND unit_id = ?", itTenant, unitID).Scan(&id).Error; err != nil {
		t.Fatalf("baca schedule id: %v", err)
	}
	return id
}

// Apply FULL: kredit == sisa cicilan → cicilan lunas, saldo habis.
func TestIntegration_ApplyCredit_Full(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 500_000_000, 0)
	ccSeedBuyerCredit(t, db, unit, 500_000_000)
	sid := ccScheduleID(t, db, unit)

	res, err := itNewRepo(db).ApplyCredit(context.Background(), itTenant, cid, sale.ApplyCreditRequest{ScheduleID: sid, Reason: "pakai penuh"})
	if err != nil {
		t.Fatalf("ApplyCredit: %v", err)
	}
	if res.Applied != "500000000" || !res.ScheduleFullyPaid || res.RemainingCredit != "0" {
		t.Errorf("hasil salah: %+v", res)
	}
	if n := itCount(t, db, "credit_applications"); n != 1 {
		t.Errorf("credit_applications: got %d, want 1", n)
	}
	// Satu baris alokasi credit_application (termin NULL).
	var caAlloc int64
	db.Table("payment_allocations").Where("tenant_id=? AND allocation_type='credit_application' AND termin_payment_id IS NULL", itTenant).Count(&caAlloc)
	if caAlloc != 1 {
		t.Errorf("baris credit_application: got %d, want 1", caAlloc)
	}
	// Cache paid_amount + status.
	var paid domain.Money
	var status string
	db.Table("payment_schedules").Select("paid_amount").Where("id=?", sid).Scan(&paid)
	db.Table("payment_schedules").Select("status").Where("id=?", sid).Scan(&status)
	if paid.String() != "500000000" || status != "received" {
		t.Errorf("cicilan: paid=%s status=%s, want 500000000/received", paid.String(), status)
	}
	// TIDAK ada jurnal baru (no cash movement).
	if n := itCount(t, db, "journal_entries"); n != 0 {
		t.Errorf("journal_entries harus 0 (apply credit tanpa jurnal), got %d", n)
	}
	// available = 0.
	view, _ := itNewRepo(db).GetBuyerCredit(context.Background(), itTenant, unit)
	if view.Available != "0" {
		t.Errorf("available: got %s, want 0", view.Available)
	}
}

// Apply PARTIAL: kredit sebagian sisa cicilan.
func TestIntegration_ApplyCredit_Partial(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 1_000_000_000, 0)
	ccSeedBuyerCredit(t, db, unit, 500_000_000)
	sid := ccScheduleID(t, db, unit)

	amt := domain.FromInt(300_000_000)
	res, err := itNewRepo(db).ApplyCredit(context.Background(), itTenant, cid, sale.ApplyCreditRequest{ScheduleID: sid, Amount: &amt})
	if err != nil {
		t.Fatalf("ApplyCredit: %v", err)
	}
	if res.Applied != "300000000" || res.ScheduleFullyPaid || res.RemainingCredit != "200000000" {
		t.Errorf("hasil salah: %+v", res)
	}
	view, _ := itNewRepo(db).GetBuyerCredit(context.Background(), itTenant, unit)
	if view.Available != "200000000" || view.TotalApplied != "300000000" {
		t.Errorf("saldo: available=%s applied=%s", view.Available, view.TotalApplied)
	}
}

// Kredit > sisa cicilan → ErrScheduleOverpaid.
func TestIntegration_ApplyCredit_ExceedsRemaining(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 300_000_000, 0)
	ccSeedBuyerCredit(t, db, unit, 1_000_000_000)
	sid := ccScheduleID(t, db, unit)

	amt := domain.FromInt(400_000_000)
	_, err := itNewRepo(db).ApplyCredit(context.Background(), itTenant, cid, sale.ApplyCreditRequest{ScheduleID: sid, Amount: &amt})
	if err != sale.ErrScheduleOverpaid {
		t.Fatalf("err: got %v, want ErrScheduleOverpaid", err)
	}
	if n := itCount(t, db, "credit_applications"); n != 0 {
		t.Errorf("tidak boleh ada credit_application, got %d", n)
	}
}

// Kredit > saldo tersedia → ErrCreditExceedsAvailable.
func TestIntegration_ApplyCredit_ExceedsAvailable(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 1_000_000_000, 0)
	ccSeedBuyerCredit(t, db, unit, 200_000_000)
	sid := ccScheduleID(t, db, unit)

	amt := domain.FromInt(500_000_000)
	_, err := itNewRepo(db).ApplyCredit(context.Background(), itTenant, cid, sale.ApplyCreditRequest{ScheduleID: sid, Amount: &amt})
	if err != sale.ErrCreditExceedsAvailable {
		t.Fatalf("err: got %v, want ErrCreditExceedsAvailable", err)
	}
}

// Idempotency: kunci sama → hanya 1 aplikasi.
func TestIntegration_ApplyCredit_Idempotent(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 1_000_000_000, 0)
	ccSeedBuyerCredit(t, db, unit, 500_000_000)
	sid := ccScheduleID(t, db, unit)

	amt := domain.FromInt(200_000_000)
	req := sale.ApplyCreditRequest{ScheduleID: sid, Amount: &amt, IdempotencyKey: "cc-key-1"}
	if _, err := itNewRepo(db).ApplyCredit(context.Background(), itTenant, cid, req); err != nil {
		t.Fatalf("apply #1: %v", err)
	}
	r2, err := itNewRepo(db).ApplyCredit(context.Background(), itTenant, cid, req)
	if err != nil {
		t.Fatalf("apply #2: %v", err)
	}
	if !r2.AlreadyExisted {
		t.Error("apply #2 harus idempoten (AlreadyExisted)")
	}
	if n := itCount(t, db, "credit_applications"); n != 1 {
		t.Errorf("harus tetap 1 credit_application, got %d", n)
	}
}

// Concurrent: lock kontrak mencegah overdraw saldo.
func TestIntegration_ApplyCredit_Concurrent_NoOverdraw(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 1_000_000_000, 0)
	ccSeedBuyerCredit(t, db, unit, 500_000_000) // hanya cukup untuk 2×200jt
	sid := ccScheduleID(t, db, unit)

	const n = 4
	repo := itNewRepo(db)
	amt := domain.FromInt(200_000_000)
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			a := amt
			_, errs[idx] = repo.ApplyCredit(context.Background(), itTenant, cid, sale.ApplyCreditRequest{ScheduleID: sid, Amount: &a})
		}(i)
	}
	wg.Wait()

	success, exceed := 0, 0
	for _, e := range errs {
		switch e {
		case nil:
			success++
		case sale.ErrCreditExceedsAvailable:
			exceed++
		default:
			t.Fatalf("error tak terduga: %v", e)
		}
	}
	if success != 2 || exceed != 2 {
		t.Errorf("serialisasi salah: success=%d exceed=%d, want 2/2", success, exceed)
	}
	// Tak pernah overdraw: available >= 0 dan == 100jt.
	view, _ := repo.GetBuyerCredit(context.Background(), itTenant, unit)
	if view.Available != "100000000" {
		t.Errorf("available: got %s, want 100000000 (tanpa overdraw)", view.Available)
	}
	if n := itCount(t, db, "credit_applications"); n != 2 {
		t.Errorf("credit_applications: got %d, want 2", n)
	}
}

// failingInvoiceTx selalu gagal — memaksa rollback setelah insert alokasi.
type failingInvoiceTx struct{}

func (failingInvoiceTx) MarkPaidByScheduleIDInTx(_ context.Context, _ *gorm.DB, _, _ uint64) error {
	return errInjected
}

// Rollback: insert/sinkron gagal → credit_application + alokasi + paid_amount tak berubah.
func TestIntegration_ApplyCredit_RollsBack(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 500_000_000, 0)
	ccSeedBuyerCredit(t, db, unit, 500_000_000)
	sid := ccScheduleID(t, db, unit)

	repo := itNewRepo(db)
	repo.SetInvoiceTxUpdater(failingInvoiceTx{}) // apply full → fully paid → invoice sync gagal

	_, err := repo.ApplyCredit(context.Background(), itTenant, cid, sale.ApplyCreditRequest{ScheduleID: sid, Reason: "full"})
	if err == nil {
		t.Fatal("ApplyCredit harus gagal (invoice sync diinjeksi gagal)")
	}
	if n := itCount(t, db, "credit_applications"); n != 0 {
		t.Errorf("credit_applications harus 0 setelah rollback, got %d", n)
	}
	var caAlloc int64
	db.Table("payment_allocations").Where("tenant_id=? AND allocation_type='credit_application'", itTenant).Count(&caAlloc)
	if caAlloc != 0 {
		t.Errorf("baris credit_application harus 0 setelah rollback, got %d", caAlloc)
	}
	var paid domain.Money
	db.Table("payment_schedules").Select("paid_amount").Where("id=?", sid).Scan(&paid)
	if paid.String() != "0" {
		t.Errorf("paid_amount harus tetap 0 setelah rollback, got %s", paid.String())
	}
}

// Guard #1 (note P2): baris credit_application (termin NULL, schedule S) TIDAK
// bentrok dengan baris schedule (termin T, schedule S) dari payment berbeda.
func TestIntegration_ApplyCredit_UniqueCoexistsWithScheduleAllocation(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 1_000_000_000, 300_000_000) // sudah dibayar 300jt (cash)
	sid := ccScheduleID(t, db, unit)

	// Baris schedule dari pembayaran kas (termin T, schedule S).
	tid := ccSeedBuyerCredit(t, db, unit, 500_000_000) // sekaligus jadi sumber kredit
	if err := db.Exec(`INSERT INTO payment_allocations (tenant_id, termin_payment_id, payment_schedule_id, allocation_type, amount)
		VALUES (?,?,?,?,?)`, itTenant, tid, sid, "schedule", domain.FromInt(300_000_000)).Error; err != nil {
		t.Fatalf("seed schedule alloc: %v", err)
	}

	// Apply credit ke cicilan yang SAMA → baris credit_application (termin NULL, schedule S).
	amt := domain.FromInt(200_000_000)
	if _, err := itNewRepo(db).ApplyCredit(context.Background(), itTenant, cid, sale.ApplyCreditRequest{ScheduleID: sid, Amount: &amt}); err != nil {
		t.Fatalf("ApplyCredit (harus lolos UNIQUE): %v", err)
	}
	// Kedua baris (schedule + credit_application) untuk cicilan S koeksis.
	var rows int64
	db.Table("payment_allocations").Where("tenant_id=? AND payment_schedule_id=?", itTenant, sid).Count(&rows)
	if rows != 2 {
		t.Errorf("harus 2 baris alokasi utk cicilan S (schedule + credit_application), got %d", rows)
	}
}
