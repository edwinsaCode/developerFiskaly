package sale_test

import (
	"context"
	"testing"
	"time"

	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// seedContractWithSchedules menyiapkan satu kontrak + jadwal cicilan pada store mock.
func seedContractWithSchedules(
	cs *mockContractStore,
	tenantID, contractID, unitID uint64,
	gross domain.Money,
	schedules []*sale.PaymentSchedule,
) {
	cs.contracts[contractID] = &sale.SaleContract{
		ID:           contractID,
		TenantID:     tenantID,
		UnitID:       unitID,
		BuyerName:    "Budi Santoso",
		BuyerID:      "3171000000000001",
		PaymentType:  sale.PaymentTypeKPR,
		ContractDate: time.Date(2026, 1, 10, 0, 0, 0, 0, time.UTC),
		DPPAmount:    gross,
		GrossAmount:  gross,
		TotalPrice:   gross,
	}
	for _, s := range schedules {
		s.TenantID = tenantID
		s.SaleContractID = contractID
		s.UnitID = unitID
		cs.schedules[s.ID] = s
		if s.ID >= cs.nextID {
			cs.nextID = s.ID
		}
	}
}

// TestGetCustomerStatement_TotalsAndOverdue memverifikasi nilai kontrak, total
// dibayar, sisa, dan deteksi tunggakan yang deterministik per asOf.
func TestGetCustomerStatement_TotalsAndOverdue(t *testing.T) {
	const tenantID, contractID, unitID = uint64(1), uint64(10), uint64(100)
	gross := domain.FromInt(1_000_000_000)

	// 4 cicilan @ 250jt. #1 received, #2 jatuh tempo (overdue), #3 & #4 belum jatuh tempo.
	asOf := time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC)
	receivedAt := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	schedules := []*sale.PaymentSchedule{
		{ID: 1, InstallmentNumber: 1, DueDate: time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), Amount: domain.FromInt(250_000_000), Type: sale.ScheduleTypeDP, Status: sale.ScheduleStatusReceived, ReceivedAt: &receivedAt},
		{ID: 2, InstallmentNumber: 2, DueDate: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Amount: domain.FromInt(250_000_000), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled},
		{ID: 3, InstallmentNumber: 3, DueDate: time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), Amount: domain.FromInt(250_000_000), Type: sale.ScheduleTypeInstallment, Status: sale.ScheduleStatusScheduled},
		{ID: 4, InstallmentNumber: 4, DueDate: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC), Amount: domain.FromInt(250_000_000), Type: sale.ScheduleTypeFinal, Status: sale.ScheduleStatusScheduled},
	}

	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenantID, contractID, unitID, gross, schedules)

	// Termin diterima = cicilan #1 (250jt).
	termins := []*sale.TerminPayment{
		{UnitID: unitID, Amount: domain.FromInt(250_000_000)},
	}
	svc, _, _ := buildServiceWithContracts(nil, termins, cs)

	stmt, err := svc.GetCustomerStatement(context.Background(), tenantID, contractID, asOf)
	if err != nil {
		t.Fatalf("GetCustomerStatement: %v", err)
	}

	if stmt.ContractValue != "1000000000" {
		t.Errorf("ContractValue: got %s, want 1000000000", stmt.ContractValue)
	}
	if stmt.TotalScheduled != "1000000000" {
		t.Errorf("TotalScheduled: got %s, want 1000000000", stmt.TotalScheduled)
	}
	if stmt.TotalPaid != "250000000" {
		t.Errorf("TotalPaid: got %s, want 250000000", stmt.TotalPaid)
	}
	if stmt.RemainingBalance != "750000000" {
		t.Errorf("RemainingBalance: got %s, want 750000000", stmt.RemainingBalance)
	}
	// Hanya cicilan #2 yang overdue (jatuh tempo 1 Mei, asOf 29 Juni).
	if stmt.OverdueCount != 1 {
		t.Errorf("OverdueCount: got %d, want 1", stmt.OverdueCount)
	}
	if stmt.TotalOverdue != "250000000" {
		t.Errorf("TotalOverdue: got %s, want 250000000", stmt.TotalOverdue)
	}
	if len(stmt.Schedules) != 4 {
		t.Fatalf("Schedules: got %d, want 4", len(stmt.Schedules))
	}

	// Cicilan #1 received → status efektif "paid", tidak overdue.
	if stmt.Schedules[0].Status != "paid" || stmt.Schedules[0].Overdue {
		t.Errorf("schedule #1 harus paid & tidak overdue, got status=%s overdue=%v", stmt.Schedules[0].Status, stmt.Schedules[0].Overdue)
	}
	// Cicilan #3 belum jatuh tempo → status "scheduled".
	if stmt.Schedules[2].Status != "scheduled" {
		t.Errorf("schedule #3 harus scheduled, got status=%s", stmt.Schedules[2].Status)
	}
	// Cicilan #2 overdue (status efektif "overdue").
	if stmt.Schedules[1].Status != "overdue" || !stmt.Schedules[1].Overdue {
		t.Errorf("schedule #2 harus overdue, got status=%s overdue=%v", stmt.Schedules[1].Status, stmt.Schedules[1].Overdue)
	}
	if stmt.Schedules[1].DaysOverdue != 59 {
		t.Errorf("schedule #2 DaysOverdue: got %d, want 59 (1 Mei → 29 Jun)", stmt.Schedules[1].DaysOverdue)
	}
	// Cicilan #3 belum jatuh tempo, tidak overdue, DaysOverdue 0.
	if stmt.Schedules[2].Overdue || stmt.Schedules[2].DaysOverdue != 0 {
		t.Errorf("schedule #3 belum jatuh tempo: overdue=%v days=%d", stmt.Schedules[2].Overdue, stmt.Schedules[2].DaysOverdue)
	}
}

// TestGetCustomerStatement_TenantIsolation memverifikasi kontrak tenant lain
// tidak dapat diakses (Invariant #6).
func TestGetCustomerStatement_TenantIsolation(t *testing.T) {
	const tenantA, tenantB = uint64(1), uint64(2)
	gross := domain.FromInt(500_000_000)

	cs := newMockContractStore()
	seedContractWithSchedules(cs, tenantA, 10, 100, gross, []*sale.PaymentSchedule{
		{ID: 1, InstallmentNumber: 1, DueDate: time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC), Amount: gross, Type: sale.ScheduleTypeFinal, Status: sale.ScheduleStatusScheduled},
	})

	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	// Tenant B mencoba membaca kontrak milik Tenant A → harus gagal.
	_, err := svc.GetCustomerStatement(context.Background(), tenantB, 10, time.Now())
	if err == nil {
		t.Fatal("tenant B seharusnya tidak bisa membaca statement tenant A")
	}
}
