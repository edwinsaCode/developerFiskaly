package billing

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"testing"
	"time"

	"esaproperti/internal/domain"
)

// ── Mock implementations ──────────────────────────────────────────────────────

type mockContracts struct {
	// [tenantID][contractID] → ContractInfo
	byID map[uint64]map[uint64]*ContractInfo
	err  error
}

func newMockContracts() *mockContracts {
	return &mockContracts{byID: make(map[uint64]map[uint64]*ContractInfo)}
}

func (m *mockContracts) add(tenantID uint64, c *ContractInfo) {
	if m.byID[tenantID] == nil {
		m.byID[tenantID] = make(map[uint64]*ContractInfo)
	}
	m.byID[tenantID][c.ID] = c
}

func (m *mockContracts) LoadContractInfo(_ context.Context, tenantID, contractID uint64) (*ContractInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if t, ok := m.byID[tenantID]; ok {
		if c, ok := t[contractID]; ok {
			return c, nil
		}
	}
	return nil, ErrContractNotFound
}

type mockSchedules struct {
	// [tenantID][scheduleID] → ScheduleInfo
	byID map[uint64]map[uint64]*ScheduleInfo
	err  error
}

func newMockSchedules() *mockSchedules {
	return &mockSchedules{byID: make(map[uint64]map[uint64]*ScheduleInfo)}
}

func (m *mockSchedules) add(tenantID uint64, s *ScheduleInfo) {
	if m.byID[tenantID] == nil {
		m.byID[tenantID] = make(map[uint64]*ScheduleInfo)
	}
	m.byID[tenantID][s.ID] = s
}

func (m *mockSchedules) LoadScheduleInfo(_ context.Context, tenantID, scheduleID uint64) (*ScheduleInfo, error) {
	if m.err != nil {
		return nil, m.err
	}
	if t, ok := m.byID[tenantID]; ok {
		if s, ok := t[scheduleID]; ok {
			return s, nil
		}
	}
	return nil, ErrScheduleNotFound
}

type mockStore struct {
	invoices   map[uint64]*Invoice // by ID
	bySchedule map[uint64]*Invoice // by scheduleID
	nextID     uint64
	nextSeq    map[uint64]uint64 // per-tenant sequence
	createErr  error
	updateErr  error
}

func newMockStore() *mockStore {
	return &mockStore{
		invoices:   make(map[uint64]*Invoice),
		bySchedule: make(map[uint64]*Invoice),
		nextID:     1,
		nextSeq:    make(map[uint64]uint64),
	}
}

func (m *mockStore) CreateInvoice(_ context.Context, inv *Invoice) error {
	if m.createErr != nil {
		return m.createErr
	}
	m.nextSeq[inv.TenantID]++
	inv.InvoiceNumber = fmt.Sprintf("INV/%d/%06d", time.Now().Year(), m.nextSeq[inv.TenantID])
	inv.ID = m.nextID
	m.nextID++
	cp := *inv
	m.invoices[cp.ID] = &cp
	if cp.ScheduleID != nil {
		schedID := *cp.ScheduleID
		m.bySchedule[schedID] = &cp
	}
	return nil
}

func (m *mockStore) FindInvoiceByID(_ context.Context, tenantID, id uint64) (*Invoice, error) {
	inv, ok := m.invoices[id]
	if !ok || inv.TenantID != tenantID {
		return nil, ErrInvoiceNotFound
	}
	cp := *inv
	return &cp, nil
}

func (m *mockStore) ListByContract(_ context.Context, tenantID, contractID uint64) ([]*Invoice, error) {
	var result []*Invoice
	for _, inv := range m.invoices {
		if inv.TenantID == tenantID && inv.SaleContractID == contractID {
			cp := *inv
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *mockStore) FindByScheduleID(_ context.Context, tenantID, scheduleID uint64) (*Invoice, error) {
	inv, ok := m.bySchedule[scheduleID]
	if !ok || inv.TenantID != tenantID {
		return nil, ErrInvoiceNotFound
	}
	cp := *inv
	return &cp, nil
}

func (m *mockStore) UpdateStatus(_ context.Context, tenantID, id uint64, status InvoiceStatus) error {
	if m.updateErr != nil {
		return m.updateErr
	}
	inv, ok := m.invoices[id]
	if !ok || inv.TenantID != tenantID {
		return ErrInvoiceNotFound
	}
	inv.Status = status
	if inv.ScheduleID != nil {
		if bs, ok2 := m.bySchedule[*inv.ScheduleID]; ok2 {
			bs.Status = status
		}
	}
	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

const (
	tenantA uint64 = 101
	tenantB uint64 = 202
)

var invoiceNumberRe = regexp.MustCompile(`^INV/\d{4}/\d{6}$`)

func mustMoney(s string) domain.Money {
	m, err := domain.NewMoney(s)
	if err != nil {
		panic("invalid money: " + s)
	}
	return m
}

func buildService() (*Service, *mockContracts, *mockSchedules, *mockStore) {
	contracts := newMockContracts()
	schedules := newMockSchedules()
	store := newMockStore()
	svc := NewService(contracts, schedules, store, nil)
	return svc, contracts, schedules, store
}

func sampleContract(id uint64) *ContractInfo {
	return &ContractInfo{ID: id, BuyerName: "Budi Santoso", BuyerID: "KTP-123"}
}

func sampleSchedule(id, contractID uint64, amount string, dueDate time.Time, schedType string) *ScheduleInfo {
	return &ScheduleInfo{
		ID:             id,
		SaleContractID: contractID,
		UnitID:         99,
		Amount:         mustMoney(amount),
		DueDate:        dueDate,
		Type:           schedType,
		Status:         "scheduled",
	}
}

// ── QA 1: Generate Invoice from Payment Schedule ──────────────────────────────

func TestGenerateInvoice_Success_AmountMatchesSchedule(t *testing.T) {
	svc, contracts, schedules, store := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "500000000", dueDate, "installment"))

	inv, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if err != nil {
		t.Fatalf("GenerateInvoice error: %v", err)
	}

	want := mustMoney("500000000")
	if !inv.Amount.Equal(want) {
		t.Errorf("Amount: got %s, want %s", inv.Amount.String(), want.String())
	}
	if !inv.DueDate.Equal(dueDate) {
		t.Errorf("DueDate: got %v, want %v", inv.DueDate, dueDate)
	}
	if inv.SaleContractID != 10 {
		t.Errorf("SaleContractID: got %d, want 10", inv.SaleContractID)
	}
	if inv.ScheduleID == nil || *inv.ScheduleID != 20 {
		t.Errorf("ScheduleID: got %v, want 20", inv.ScheduleID)
	}
	if inv.TenantID != tenantA {
		t.Errorf("TenantID: got %d, want %d", inv.TenantID, tenantA)
	}

	// Verify saved to store
	if len(store.invoices) != 1 {
		t.Errorf("expected 1 invoice in store, got %d", len(store.invoices))
	}
}

func TestGenerateInvoice_InvoiceNumber_MatchesFormat(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "300000000", dueDate, "dp"))

	inv, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !invoiceNumberRe.MatchString(inv.InvoiceNumber) {
		t.Errorf("InvoiceNumber %q does not match INV/YYYY/NNNNNN", inv.InvoiceNumber)
	}
}

func TestGenerateInvoice_InvoiceNumber_UniquePerTenant(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(21, 10, "100000000", dueDate, "installment"))
	schedules.add(tenantA, sampleSchedule(22, 10, "100000000", dueDate, "installment"))

	inv1, _ := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 21})
	inv2, _ := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 22})

	if inv1.InvoiceNumber == inv2.InvoiceNumber {
		t.Errorf("InvoiceNumber must be unique: both got %q", inv1.InvoiceNumber)
	}
}

func TestGenerateInvoice_StatusIsIssued(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "200000000", dueDate, "installment"))

	inv, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if inv.Status != StatusIssued {
		t.Errorf("Status: got %q, want %q", inv.Status, StatusIssued)
	}
}

func TestGenerateInvoice_NoJournalCreated(t *testing.T) {
	// The billing service touches only InvoiceStore — no ledger.PostingService call.
	// This is proven by the mockStore: it has no journal-related methods.
	// GenerateInvoice success with mock store proves no journal side-effect.
	svc, contracts, schedules, store := buildService()
	dueDate := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "150000000", dueDate, "dp"))

	_, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only invoice store has data; if journals were written they would panic
	// (mock has no journal method) — success proves no journal side-effect.
	if len(store.invoices) != 1 {
		t.Errorf("expected exactly 1 invoice, got %d", len(store.invoices))
	}
}

// ── QA 1: Type mapping ────────────────────────────────────────────────────────

func TestGenerateInvoice_TypeMapping(t *testing.T) {
	cases := []struct {
		schedType string
		wantType  InvoiceType
	}{
		{"dp", TypeDP},
		{"installment", TypeTermin},
		{"final", TypePelunasan},
	}
	for _, tc := range cases {
		t.Run(tc.schedType, func(t *testing.T) {
			svc, contracts, schedules, _ := buildService()
			dueDate := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
			contracts.add(tenantA, sampleContract(10))
			schedules.add(tenantA, sampleSchedule(20, 10, "100000000", dueDate, tc.schedType))

			inv, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
				ScheduleID: 20,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if inv.InvoiceType != tc.wantType {
				t.Errorf("InvoiceType: got %q, want %q", inv.InvoiceType, tc.wantType)
			}
		})
	}
}

// ── QA 2: Duplicate protection ────────────────────────────────────────────────

func TestGenerateInvoice_Duplicate_ReturnsErrAlreadyExists(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "250000000", dueDate, "installment"))

	// First call — success.
	_, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	// Second call — must fail.
	_, err = svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if !errors.Is(err, ErrInvoiceAlreadyExists) {
		t.Errorf("second call: want ErrInvoiceAlreadyExists, got %v", err)
	}
}

func TestGenerateInvoice_Duplicate_OnlyOneInvoiceStored(t *testing.T) {
	svc, contracts, schedules, store := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "250000000", dueDate, "installment"))

	svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20}) //nolint
	svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20}) //nolint

	if len(store.invoices) != 1 {
		t.Errorf("expected exactly 1 invoice in store, got %d", len(store.invoices))
	}
}

// ── QA 2: Error cases ─────────────────────────────────────────────────────────

func TestGenerateInvoice_ContractNotFound(t *testing.T) {
	svc, _, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	schedules.add(tenantA, sampleSchedule(20, 10, "100000000", dueDate, "installment"))

	_, err := svc.GenerateInvoice(context.Background(), tenantA, 999, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if !errors.Is(err, ErrContractNotFound) {
		t.Errorf("want ErrContractNotFound, got %v", err)
	}
}

func TestGenerateInvoice_ScheduleNotFound(t *testing.T) {
	svc, contracts, _, _ := buildService()
	contracts.add(tenantA, sampleContract(10))

	_, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 999,
	})
	if !errors.Is(err, ErrScheduleNotFound) {
		t.Errorf("want ErrScheduleNotFound, got %v", err)
	}
}

func TestGenerateInvoice_ContractMismatch(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	// Contract 10, schedule belongs to contract 99 (different).
	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 99, "100000000", dueDate, "installment"))

	_, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if !errors.Is(err, ErrScheduleContractMismatch) {
		t.Errorf("want ErrScheduleContractMismatch, got %v", err)
	}
}

func TestGenerateInvoice_AlreadyReceived_Rejected(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	s := sampleSchedule(20, 10, "100000000", dueDate, "installment")
	s.Status = "received"
	schedules.add(tenantA, s)

	_, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if !errors.Is(err, ErrScheduleAlreadyReceived) {
		t.Errorf("want ErrScheduleAlreadyReceived, got %v", err)
	}
}

// ── QA 3: Tenant isolation ────────────────────────────────────────────────────

func TestTenantIsolation_Generate_TenantBCannotUseContractA(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	// Contract and schedule belong to tenantA only.
	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "100000000", dueDate, "installment"))

	// TenantB tries to generate against tenantA's contract.
	_, err := svc.GenerateInvoice(context.Background(), tenantB, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
	})
	if !errors.Is(err, ErrContractNotFound) {
		t.Errorf("want ErrContractNotFound for cross-tenant, got %v", err)
	}
}

func TestTenantIsolation_ListByContract_TenantBSeesEmpty(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "100000000", dueDate, "installment"))

	// TenantA creates an invoice.
	svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20}) //nolint

	// TenantB lists same contractID — must get zero results.
	list, err := svc.ListByContract(context.Background(), tenantB, 10)
	if err != nil {
		t.Fatalf("ListByContract error: %v", err)
	}
	if len(list) != 0 {
		t.Errorf("TenantB should see 0 invoices for TenantA's contract, got %d", len(list))
	}
}

func TestTenantIsolation_GetInvoice_TenantBCrossRead(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "100000000", dueDate, "installment"))

	inv, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	// TenantB reads tenantA's invoice by ID.
	_, err = svc.GetInvoice(context.Background(), tenantB, inv.ID)
	if !errors.Is(err, ErrInvoiceNotFound) {
		t.Errorf("want ErrInvoiceNotFound for cross-tenant read, got %v", err)
	}
}

func TestTenantIsolation_MarkPaid_CrossTenant_NoEffect(t *testing.T) {
	svc, contracts, schedules, store := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "100000000", dueDate, "installment"))

	svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20}) //nolint

	// TenantB tries to mark paid for scheduleID=20 — it belongs to tenantA.
	err := svc.MarkPaidByScheduleID(context.Background(), tenantB, 20)
	if err != nil {
		t.Fatalf("MarkPaidByScheduleID with wrong tenant: %v", err)
	}

	// TenantA's invoice must still be "issued".
	var found *Invoice
	for _, inv := range store.invoices {
		found = inv
	}
	if found.Status != StatusIssued {
		t.Errorf("TenantA invoice should still be issued, got %q", found.Status)
	}
}

// ── QA 4: Status flow ─────────────────────────────────────────────────────────

func TestStatusFlow_MarkPaidByScheduleID_IssuedToPaid(t *testing.T) {
	svc, contracts, schedules, store := buildService()
	dueDate := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "300000000", dueDate, "installment"))

	inv, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20})
	if err != nil {
		t.Fatalf("setup: %v", err)
	}
	if inv.Status != StatusIssued {
		t.Fatalf("precondition: expected status issued, got %q", inv.Status)
	}

	if err := svc.MarkPaidByScheduleID(context.Background(), tenantA, 20); err != nil {
		t.Fatalf("MarkPaidByScheduleID: %v", err)
	}

	updated := store.invoices[inv.ID]
	if updated.Status != StatusPaid {
		t.Errorf("Status after MarkPaid: got %q, want %q", updated.Status, StatusPaid)
	}
}

func TestStatusFlow_MarkPaid_AlreadyPaid_Idempotent(t *testing.T) {
	svc, contracts, schedules, store := buildService()
	dueDate := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "300000000", dueDate, "installment"))

	inv, _ := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20})
	store.invoices[inv.ID].Status = StatusPaid

	// Second MarkPaid — should be no-op.
	if err := svc.MarkPaidByScheduleID(context.Background(), tenantA, 20); err != nil {
		t.Fatalf("second MarkPaid: %v", err)
	}
	if store.invoices[inv.ID].Status != StatusPaid {
		t.Errorf("Status should remain paid, got %q", store.invoices[inv.ID].Status)
	}
}

func TestStatusFlow_MarkPaid_Cancelled_NotUpdated(t *testing.T) {
	svc, contracts, schedules, store := buildService()
	dueDate := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, "300000000", dueDate, "installment"))

	inv, _ := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 20})
	store.invoices[inv.ID].Status = StatusCancelled

	if err := svc.MarkPaidByScheduleID(context.Background(), tenantA, 20); err != nil {
		t.Fatalf("MarkPaid on cancelled: %v", err)
	}
	if store.invoices[inv.ID].Status != StatusCancelled {
		t.Errorf("Cancelled invoice should stay cancelled, got %q", store.invoices[inv.ID].Status)
	}
}

func TestStatusFlow_MarkPaid_NoInvoice_SilentNoOp(t *testing.T) {
	svc, _, _, _ := buildService()

	// No invoice for schedule 999 — must return nil.
	err := svc.MarkPaidByScheduleID(context.Background(), tenantA, 999)
	if err != nil {
		t.Errorf("expected nil error when no invoice found, got %v", err)
	}
}

// ── QA 5: Print invoice field correctness ─────────────────────────────────────
// The print function reads from Invoice struct. We validate Invoice fields are
// correctly populated from the schedule so the printed document is accurate.

func TestGenerateInvoice_PrintFields_AmountAndDueDateFromSchedule(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 12, 31, 0, 0, 0, 0, time.UTC)
	wantAmount := "750000000"

	contracts.add(tenantA, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(20, 10, wantAmount, dueDate, "final"))

	inv, err := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{
		ScheduleID: 20,
		Notes:      "Pelunasan unit blok A",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !inv.Amount.Equal(mustMoney(wantAmount)) {
		t.Errorf("Print: Amount mismatch — got %s, want %s", inv.Amount.String(), wantAmount)
	}
	if !inv.DueDate.Equal(dueDate) {
		t.Errorf("Print: DueDate mismatch — got %v, want %v", inv.DueDate, dueDate)
	}
	if inv.InvoiceType != TypePelunasan {
		t.Errorf("Print: InvoiceType mismatch — got %q, want %q", inv.InvoiceType, TypePelunasan)
	}
	if inv.Notes != "Pelunasan unit blok A" {
		t.Errorf("Print: Notes mismatch — got %q", inv.Notes)
	}
}

// ── Sequence isolation between tenants ───────────────────────────────────────

func TestInvoiceNumber_PerTenantSequence(t *testing.T) {
	svc, contracts, schedules, _ := buildService()
	dueDate := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)

	contracts.add(tenantA, sampleContract(10))
	contracts.add(tenantB, sampleContract(10))
	schedules.add(tenantA, sampleSchedule(21, 10, "100000000", dueDate, "installment"))
	schedules.add(tenantB, sampleSchedule(31, 10, "200000000", dueDate, "installment"))

	invA, _ := svc.GenerateInvoice(context.Background(), tenantA, 10, 1, GenerateInvoiceRequest{ScheduleID: 21})
	invB, _ := svc.GenerateInvoice(context.Background(), tenantB, 10, 1, GenerateInvoiceRequest{ScheduleID: 31})

	// Both should be sequence 1 (each tenant has its own counter).
	wantSuffix := fmt.Sprintf("000001")
	if !contains(invA.InvoiceNumber, wantSuffix) {
		t.Errorf("TenantA first invoice should end with %s, got %s", wantSuffix, invA.InvoiceNumber)
	}
	if !contains(invB.InvoiceNumber, wantSuffix) {
		t.Errorf("TenantB first invoice should end with %s, got %s", wantSuffix, invB.InvoiceNumber)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && (s[len(s)-len(substr):] == substr))
}

// ── R1: Invoice Kekurangan Pembayaran ─────────────────────────────────────────

type stubSummaryProvider struct{ out string }

func (s *stubSummaryProvider) SummaryByContractID(_ context.Context, _, _ uint64) (*ContractSummary, error) {
	return &ContractSummary{Outstanding: mustMoney(s.out)}, nil
}
func (s *stubSummaryProvider) SummaryByUnitID(_ context.Context, _, _ uint64) (*ContractSummary, error) {
	return &ContractSummary{Outstanding: mustMoney(s.out)}, nil
}

func TestGenerateShortfallInvoice_AmountFromSummary(t *testing.T) {
	svc, contracts, _, store := buildService()
	contracts.add(tenantA, sampleContract(7))
	svc.SetContractSummaryProvider(&stubSummaryProvider{out: "20000000"})

	inv, err := svc.GenerateShortfallInvoice(context.Background(), tenantA, 7, 1, time.Time{}, "")
	if err != nil {
		t.Fatalf("GenerateShortfallInvoice: %v", err)
	}
	if inv.InvoiceType != TypeKekurangan {
		t.Errorf("type = %s, want KEKURANGAN", inv.InvoiceType)
	}
	if inv.Amount.String() != "20000000" {
		t.Errorf("amount = %s, want 20000000 (dari summary — satu rumus)", inv.Amount)
	}
	if inv.ScheduleID != nil {
		t.Error("invoice kekurangan tidak terikat cicilan (ScheduleID nil)")
	}
	_ = store
}

func TestGenerateShortfallInvoice_RejectsZeroOutstanding(t *testing.T) {
	svc, contracts, _, _ := buildService()
	contracts.add(tenantA, sampleContract(7))
	svc.SetContractSummaryProvider(&stubSummaryProvider{out: "0"})

	if _, err := svc.GenerateShortfallInvoice(context.Background(), tenantA, 7, 1, time.Time{}, ""); !errors.Is(err, ErrNoOutstanding) {
		t.Fatalf("outstanding 0 harus ErrNoOutstanding, got %v", err)
	}
}

func TestGenerateShortfallInvoice_DedupUnpaid(t *testing.T) {
	svc, contracts, _, _ := buildService()
	contracts.add(tenantA, sampleContract(7))
	svc.SetContractSummaryProvider(&stubSummaryProvider{out: "20000000"})

	if _, err := svc.GenerateShortfallInvoice(context.Background(), tenantA, 7, 1, time.Time{}, ""); err != nil {
		t.Fatalf("pertama: %v", err)
	}
	// Kedua saat yang pertama belum dibayar → ditolak (cegah tagihan ganda).
	if _, err := svc.GenerateShortfallInvoice(context.Background(), tenantA, 7, 1, time.Time{}, ""); !errors.Is(err, ErrShortfallInvoiceExists) {
		t.Fatalf("kedua harus ErrShortfallInvoiceExists, got %v", err)
	}
}

// R1: pelunasan kontrak → invoice KEKURANGAN terbuka ikut paid; setelah itu
// kekurangan baru boleh dibuat lagi bila muncul outstanding baru.
func TestSettleShortfallIfPaid(t *testing.T) {
	svc, contracts, _, store := buildService()
	contracts.add(tenantA, sampleContract(7))
	sum := &stubSummaryProvider{out: "20000000"}
	svc.SetContractSummaryProvider(sum)

	inv, err := svc.GenerateShortfallInvoice(context.Background(), tenantA, 7, 1, time.Time{}, "")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}

	// Outstanding masih ada → settle TIDAK menandai paid.
	svc.SettleShortfallIfPaid(context.Background(), tenantA, 7)
	if got := store.invoices[inv.ID].Status; got != StatusIssued {
		t.Fatalf("outstanding >0: status = %s, want issued", got)
	}

	// Kontrak lunas → invoice kekurangan otomatis paid.
	sum.out = "0"
	svc.SettleShortfallIfPaid(context.Background(), tenantA, 7)
	if got := store.invoices[inv.ID].Status; got != StatusPaid {
		t.Fatalf("outstanding 0: status = %s, want paid", got)
	}

	// Setelah paid, dedup terbuka: kekurangan baru boleh dibuat lagi.
	sum.out = "5000000"
	if _, err := svc.GenerateShortfallInvoice(context.Background(), tenantA, 7, 1, time.Time{}, ""); err != nil {
		t.Fatalf("kekurangan baru pasca-settle harus boleh: %v", err)
	}
}
