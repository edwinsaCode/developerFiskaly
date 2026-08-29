package sale

import (
	"testing"
	"time"

	"esaproperti/internal/domain"
)

// FE-2 · P3 — unit test murni untuk replay + rekonsiliasi (planUnitBackfill).

func bfSched(id uint64, num int, amount, paid int64) *PaymentSchedule {
	return &PaymentSchedule{
		ID: id, InstallmentNumber: num,
		DueDate: time.Date(2026, time.Month(num+1), 1, 0, 0, 0, 0, time.UTC),
		Amount:  domain.FromInt(amount), PaidAmount: domain.FromInt(paid),
		Type: ScheduleTypeInstallment, Status: ScheduleStatusScheduled,
	}
}

func bfTermin(id uint64, amount int64, day int) *TerminPayment {
	return &TerminPayment{ID: id, Amount: domain.FromInt(amount), Date: time.Date(2026, 3, day, 0, 0, 0, 0, time.UTC)}
}

// Replay waterfall menghasilkan paid_amount yang cocok → reconciled + alokasi benar.
func TestPlanUnitBackfill_Reconciled(t *testing.T) {
	schedules := []*PaymentSchedule{
		bfSched(1, 1, 500_000_000, 500_000_000),
		bfSched(2, 2, 500_000_000, 300_000_000),
	}
	termins := []*TerminPayment{
		bfTermin(2, 500_000_000, 20), // sengaja tak urut: harus di-sort by date
		bfTermin(1, 300_000_000, 10),
	}

	plan := planUnitBackfill(100, 9, termins, schedules)
	if !plan.reconciled {
		t.Fatalf("harus reconciled, reason=%q detail=%q", plan.reason, plan.detail)
	}
	if len(plan.termins) != 2 {
		t.Fatalf("harus 2 termin, got %d", len(plan.termins))
	}
	// Setelah sort: termin #1 (300jt, tgl 10) dulu → 1 alokasi ke cicilan 1.
	t1 := plan.termins[0]
	if t1.terminID != 1 || len(t1.schedule) != 1 || t1.schedule[0].ScheduleID != 1 || t1.schedule[0].Apply.String() != "300000000" {
		t.Errorf("replay termin #1 salah: %+v", t1)
	}
	// termin #2 (500jt) → 200jt menutup cicilan 1 + 300jt ke cicilan 2.
	t2 := plan.termins[1]
	if t2.terminID != 2 || len(t2.schedule) != 2 {
		t.Fatalf("replay termin #2 harus 2 alokasi: %+v", t2)
	}
	if t2.schedule[0].ScheduleID != 1 || t2.schedule[0].Apply.String() != "200000000" ||
		t2.schedule[1].ScheduleID != 2 || t2.schedule[1].Apply.String() != "300000000" {
		t.Errorf("replay termin #2 salah: %+v", t2.schedule)
	}
	if !t2.buyerCredit.IsZero() {
		t.Errorf("tidak boleh ada buyer_credit: %s", t2.buyerCredit)
	}
}

// paid_amount aktual tak bisa direproduksi waterfall (mis. dulu targeting cicilan
// spesifik) → FLAGGED, tidak reconciled (no auto-merge).
func TestPlanUnitBackfill_Mismatch_Flagged(t *testing.T) {
	schedules := []*PaymentSchedule{
		bfSched(1, 1, 500_000_000, 0),           // aktual: belum dibayar
		bfSched(2, 2, 500_000_000, 500_000_000), // aktual: lunas (targeting non-waterfall)
	}
	termins := []*TerminPayment{bfTermin(1, 500_000_000, 10)}

	plan := planUnitBackfill(100, 9, termins, schedules)
	if plan.reconciled {
		t.Fatal("harus flagged (mismatch), bukan reconciled")
	}
	if plan.reason != "paid_amount mismatch" || plan.detail == "" {
		t.Errorf("reason/detail kurang informatif: reason=%q detail=%q", plan.reason, plan.detail)
	}
}

// Tanpa cicilan (advance tanpa kontrak) → tiap termin jadi buyer_credit; reconciled.
func TestPlanUnitBackfill_NoSchedules_BuyerCredit(t *testing.T) {
	termins := []*TerminPayment{bfTermin(1, 250_000_000, 10), bfTermin(2, 100_000_000, 12)}

	plan := planUnitBackfill(100, 0, termins, nil)
	if !plan.reconciled {
		t.Fatal("tanpa cicilan harus reconciled")
	}
	if len(plan.termins) != 2 {
		t.Fatalf("harus 2 termin, got %d", len(plan.termins))
	}
	for _, tp := range plan.termins {
		if len(tp.schedule) != 0 {
			t.Errorf("tidak boleh ada alokasi cicilan: %+v", tp.schedule)
		}
	}
	if plan.termins[0].buyerCredit.String() != "250000000" || plan.termins[1].buyerCredit.String() != "100000000" {
		t.Errorf("buyer_credit salah: %s, %s", plan.termins[0].buyerCredit, plan.termins[1].buyerCredit)
	}
}
