//go:build integration

package sale_test

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// FE-2 · P3 — integration test backfill terhadap MySQL nyata.
// Prasyarat sama dgn payment_allocation_integration_test.go (TEST_DB_DSN + migrated).

func bfSeedContract(t *testing.T, db *gorm.DB, unitID uint64) uint64 {
	t.Helper()
	c := &sale.SaleContract{
		TenantID: itTenant, UnitID: unitID, BuyerName: "Backfill", BuyerID: "1",
		PaymentType: sale.PaymentTypeKPR, ContractDate: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		DPPAmount: domain.FromInt(1_000_000_000), GrossAmount: domain.FromInt(1_000_000_000), TotalPrice: domain.FromInt(1_000_000_000),
	}
	if err := db.Create(c).Error; err != nil {
		t.Fatalf("seed contract: %v", err)
	}
	return c.ID
}

func bfSeedScheduleRow(t *testing.T, db *gorm.DB, contractID, unitID uint64, num int, amount, paid int64) {
	t.Helper()
	s := &sale.PaymentSchedule{
		TenantID: itTenant, SaleContractID: contractID, UnitID: unitID, InstallmentNumber: num,
		DueDate: time.Date(2026, time.Month(num+1), 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(amount), PaidAmount: domain.FromInt(paid),
		Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled,
	}
	if err := db.Create(s).Error; err != nil {
		t.Fatalf("seed schedule: %v", err)
	}
}

func bfSeedTerminRow(t *testing.T, db *gorm.DB, unitID uint64, amount int64, day int) {
	t.Helper()
	tp := &sale.TerminPayment{
		TenantID: itTenant, UnitID: unitID, ProjectID: 10, Amount: domain.FromInt(amount),
		BankAccountCode: "1-1300", Date: time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC),
		JournalEntryID: 1, CreditAccountCode: "2-2000",
	}
	if err := db.Create(tp).Error; err != nil {
		t.Fatalf("seed termin: %v", err)
	}
}

// Reconciled → apply menulis alokasi; re-run idempoten (skip, tak menambah baris).
func TestIntegration_Backfill_ReconciledApply_Idempotent(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 500_000_000, 500_000_000)
	bfSeedScheduleRow(t, db, cid, unit, 2, 500_000_000, 300_000_000)
	bfSeedTerminRow(t, db, unit, 300_000_000, 10)
	bfSeedTerminRow(t, db, unit, 500_000_000, 20)

	repo := sale.NewGORMRepository(db, nil, nil)
	rep, err := repo.BackfillAllocations(context.Background(), itTenant, true)
	if err != nil {
		t.Fatalf("backfill apply: %v", err)
	}
	if rep.UnitsReconciled != 1 || rep.UnitsFlagged != 0 {
		t.Errorf("rekonsiliasi: reconciled=%d flagged=%d, want 1/0", rep.UnitsReconciled, rep.UnitsFlagged)
	}
	if rep.TerminsBackfilled != 2 || rep.AllocationsToWrite != 3 {
		t.Errorf("backfill: termins=%d allocs=%d, want 2/3", rep.TerminsBackfilled, rep.AllocationsToWrite)
	}
	if n := itCount(t, db, "payment_allocations"); n != 3 {
		t.Errorf("payment_allocations: got %d, want 3", n)
	}

	// Re-run: idempoten — semua termin dilewati, tak ada baris baru.
	rep2, err := repo.BackfillAllocations(context.Background(), itTenant, true)
	if err != nil {
		t.Fatalf("backfill re-run: %v", err)
	}
	if rep2.TerminsSkipped != 2 || rep2.TerminsBackfilled != 0 {
		t.Errorf("re-run harus idempoten: skipped=%d backfilled=%d, want 2/0", rep2.TerminsSkipped, rep2.TerminsBackfilled)
	}
	if n := itCount(t, db, "payment_allocations"); n != 3 {
		t.Errorf("payment_allocations setelah re-run: got %d, want 3 (tetap)", n)
	}
}

// Mismatch → flagged, tidak menulis apa pun (no auto-merge).
func TestIntegration_Backfill_Mismatch_Flagged(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(6)
	cid := bfSeedContract(t, db, unit)
	// Aktual: cicilan 2 lunas, cicilan 1 kosong — TIDAK bisa direproduksi waterfall.
	bfSeedScheduleRow(t, db, cid, unit, 1, 500_000_000, 0)
	bfSeedScheduleRow(t, db, cid, unit, 2, 500_000_000, 500_000_000)
	bfSeedTerminRow(t, db, unit, 500_000_000, 10)

	repo := sale.NewGORMRepository(db, nil, nil)
	rep, err := repo.BackfillAllocations(context.Background(), itTenant, true)
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if rep.UnitsFlagged != 1 || rep.UnitsReconciled != 0 {
		t.Errorf("harus flagged: flagged=%d reconciled=%d, want 1/0", rep.UnitsFlagged, rep.UnitsReconciled)
	}
	if len(rep.Flags) != 1 || rep.Flags[0].UnitID != unit {
		t.Errorf("flag unit salah: %+v", rep.Flags)
	}
	if n := itCount(t, db, "payment_allocations"); n != 0 {
		t.Errorf("unit flagged tidak boleh menulis alokasi, got %d", n)
	}
}

// Dry-run → melaporkan rencana tanpa menulis.
func TestIntegration_Backfill_DryRun_NoWrite(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	bfSeedScheduleRow(t, db, cid, unit, 1, 500_000_000, 500_000_000)
	bfSeedScheduleRow(t, db, cid, unit, 2, 500_000_000, 300_000_000)
	bfSeedTerminRow(t, db, unit, 300_000_000, 10)
	bfSeedTerminRow(t, db, unit, 500_000_000, 20)

	repo := sale.NewGORMRepository(db, nil, nil)
	rep, err := repo.BackfillAllocations(context.Background(), itTenant, false)
	if err != nil {
		t.Fatalf("backfill dry-run: %v", err)
	}
	if rep.AllocationsToWrite != 3 || rep.TerminsBackfilled != 2 {
		t.Errorf("dry-run rencana: allocs=%d termins=%d, want 3/2", rep.AllocationsToWrite, rep.TerminsBackfilled)
	}
	if n := itCount(t, db, "payment_allocations"); n != 0 {
		t.Errorf("dry-run tidak boleh menulis, got %d baris", n)
	}
}
