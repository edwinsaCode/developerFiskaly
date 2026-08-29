//go:build integration

package sale_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// FE-2 · P4 — integration test reader alokasi (join termin + cicilan + kwitansi).

func TestIntegration_ListAllocations_Enriched(t *testing.T) {
	db := itConnect(t)
	itCleanup(t, db)
	defer itCleanup(t, db)
	// receipts tidak dibersihkan itCleanup → bersihkan manual.
	db.Exec("DELETE FROM receipts WHERE tenant_id = ?", itTenant)
	defer db.Exec("DELETE FROM receipts WHERE tenant_id = ?", itTenant)

	const unit = uint64(5)
	cid := bfSeedContract(t, db, unit)
	// Cicilan 2 (installment) — untuk label "Termin 2".
	bfSeedScheduleRow(t, db, cid, unit, 2, 500_000_000, 300_000_000)
	// Termin + kwitansi.
	tp := &sale.TerminPayment{
		TenantID: itTenant, UnitID: unit, ProjectID: 10, Amount: domain.FromInt(400_000_000),
		BankAccountCode: "1-1300", Date: time.Date(2026, 3, 15, 0, 0, 0, 0, time.UTC),
		JournalEntryID: 1, CreditAccountCode: "2-2000",
	}
	if err := db.Create(tp).Error; err != nil {
		t.Fatalf("seed termin: %v", err)
	}
	if err := db.Exec(`INSERT INTO receipts (tenant_id, termin_payment_id, unit_id, receipt_number, amount, bank_account_code, received_at, created_by)
		VALUES (?,?,?,?,?,?,?,?)`,
		itTenant, tp.ID, unit, "KWT/2026/000042", "400000000.0000", "1-1300", tp.Date, 1).Error; err != nil {
		t.Fatalf("seed receipt: %v", err)
	}

	// Ambil schedule id.
	var schedID uint64
	if err := db.Table("payment_schedules").Select("id").
		Where("tenant_id = ? AND unit_id = ?", itTenant, unit).Scan(&schedID).Error; err != nil {
		t.Fatalf("baca schedule id: %v", err)
	}
	// Alokasi: 300jt ke cicilan 2 + 100jt buyer_credit.
	if err := db.Exec(`INSERT INTO payment_allocations (tenant_id, termin_payment_id, payment_schedule_id, allocation_type, amount)
		VALUES (?,?,?,?,?),(?,?,?,?,?)`,
		itTenant, tp.ID, schedID, "schedule", "300000000.0000",
		itTenant, tp.ID, nil, "buyer_credit", "100000000.0000").Error; err != nil {
		t.Fatalf("seed alokasi: %v", err)
	}

	repo := sale.NewGORMRepository(db, nil, nil)
	views, err := repo.ListAllocationsByUnit(context.Background(), itTenant, unit)
	if err != nil {
		t.Fatalf("ListAllocationsByUnit: %v", err)
	}
	if len(views) != 2 {
		t.Fatalf("harus 2 baris, got %d: %+v", len(views), views)
	}

	var sched, credit *sale.AllocationView
	for i := range views {
		switch views[i].AllocationType {
		case "schedule":
			sched = &views[i]
		case "buyer_credit":
			credit = &views[i]
		}
	}
	if sched == nil || credit == nil {
		t.Fatalf("baris schedule/buyer_credit tidak lengkap: %+v", views)
	}
	// Enrichment schedule.
	if sched.Label != "Termin 2" {
		t.Errorf("label schedule: got %q, want 'Termin 2'", sched.Label)
	}
	if sched.Amount != "300000000" {
		t.Errorf("amount schedule: got %q, want 300000000", sched.Amount)
	}
	if sched.ReceiptNumber != "KWT/2026/000042" {
		t.Errorf("receipt_number: got %q, want KWT/2026/000042", sched.ReceiptNumber)
	}
	if sched.TerminDate != "2026-03-15" {
		t.Errorf("termin_date: got %q, want 2026-03-15", sched.TerminDate)
	}
	if sched.InstallmentNumber == nil || *sched.InstallmentNumber != 2 {
		t.Errorf("installment_number: got %v, want 2", sched.InstallmentNumber)
	}
	// Enrichment buyer_credit.
	if credit.Label != "Saldo Kredit Buyer" {
		t.Errorf("label buyer_credit: got %q", credit.Label)
	}
	if credit.PaymentScheduleID != nil {
		t.Errorf("buyer_credit tidak boleh punya schedule id: %v", credit.PaymentScheduleID)
	}
	if credit.InstallmentNumber != nil {
		t.Errorf("buyer_credit tidak boleh punya installment_number: %v", credit.InstallmentNumber)
	}

	// ByTermin mengembalikan baris yang sama.
	byTermin, err := repo.ListAllocationsByTermin(context.Background(), itTenant, tp.ID)
	if err != nil {
		t.Fatalf("ListAllocationsByTermin: %v", err)
	}
	if len(byTermin) != 2 {
		t.Errorf("ByTermin harus 2 baris, got %d", len(byTermin))
	}
}
