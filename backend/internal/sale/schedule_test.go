package sale_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
	"esaproperti/internal/project"
	"esaproperti/internal/sale"
)

// ── mockContractStore ─────────────────────────────────────────────────────────

type mockContractStore struct {
	mu        sync.Mutex
	contracts map[uint64]*sale.SaleContract
	schedules map[uint64]*sale.PaymentSchedule
	nextID    uint64
	// allocations mencatat baris payment_allocations yang ditulis (untuk assertion).
	allocations []recordedAllocation
	// applyErr: bila di-set, ApplyScheduleAllocation mengembalikan error (uji guard).
	applyErr error
}

// recordedAllocation adalah snapshot satu baris alokasi sub-ledger (test).
type recordedAllocation struct {
	TerminPaymentID uint64
	ScheduleID      *uint64 // nil = buyer_credit
	Type            sale.AllocationType
	Amount          string
}

func newMockContractStore() *mockContractStore {
	return &mockContractStore{
		contracts: make(map[uint64]*sale.SaleContract),
		schedules: make(map[uint64]*sale.PaymentSchedule),
	}
}

func (m *mockContractStore) newID() uint64 {
	m.nextID++
	return m.nextID
}

func (m *mockContractStore) SaveContract(_ context.Context, c *sale.SaleContract) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c.ID == 0 {
		c.ID = m.newID()
	}
	cp := *c
	m.contracts[c.ID] = &cp
	return nil
}

func (m *mockContractStore) FindContractByID(_ context.Context, tenantID, id uint64) (*sale.SaleContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	c, ok := m.contracts[id]
	if !ok || c.TenantID != tenantID {
		return nil, sale.ErrContractNotFound
	}
	cp := *c
	return &cp, nil
}

func (m *mockContractStore) ListActiveContracts(_ context.Context, tenantID uint64) ([]*sale.SaleContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*sale.SaleContract
	for _, c := range m.contracts {
		if c.TenantID != tenantID {
			continue
		}
		if c.SchemeState != nil && *c.SchemeState == "cancelled" {
			continue
		}
		cp := *c
		out = append(out, &cp)
	}
	return out, nil
}

func (m *mockContractStore) SaveScheduleItems(_ context.Context, items []*sale.PaymentSchedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, item := range items {
		if item.ID == 0 {
			item.ID = m.newID()
		}
		cp := *item
		m.schedules[item.ID] = &cp
	}
	return nil
}

func (m *mockContractStore) FindScheduleByID(_ context.Context, tenantID, id uint64) (*sale.PaymentSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.schedules[id]
	if !ok || s.TenantID != tenantID {
		return nil, sale.ErrScheduleNotFound
	}
	cp := *s
	return &cp, nil
}

func (m *mockContractStore) UpdateScheduleStatus(_ context.Context, tenantID, id uint64, status sale.ScheduleStatus, terminPaymentID *uint64, receivedAt *time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.schedules[id]
	if !ok || s.TenantID != tenantID {
		return sale.ErrScheduleNotFound
	}
	s.Status = status
	if terminPaymentID != nil {
		s.TerminPaymentID = terminPaymentID
	}
	if receivedAt != nil {
		s.ReceivedAt = receivedAt
	}
	return nil
}

// SchedulePaidAmount mengembalikan paid_amount cicilan sebagai string (test helper).
func (m *mockContractStore) SchedulePaidAmount(id uint64) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.schedules[id]; ok {
		return s.PaidAmount.String()
	}
	return ""
}

// ApplyScheduleAllocation menulis baris alokasi + cache paid_amount (guard #1).
func (m *mockContractStore) ApplyScheduleAllocation(_ context.Context, tenantID uint64, in sale.ScheduleAllocationInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.applyErr != nil {
		return m.applyErr
	}
	s, ok := m.schedules[in.ScheduleID]
	if !ok || s.TenantID != tenantID {
		return sale.ErrScheduleNotFound
	}
	s.PaidAmount = in.NewPaid
	if in.FullyPaid {
		s.Status = sale.ScheduleStatusReceived
		tp := in.TerminPaymentID
		s.TerminPaymentID = &tp
		ra := in.ReceivedAt
		s.ReceivedAt = &ra
	}
	sid := in.ScheduleID
	m.allocations = append(m.allocations, recordedAllocation{
		TerminPaymentID: in.TerminPaymentID,
		ScheduleID:      &sid,
		Type:            sale.AllocationTypeSchedule,
		Amount:          in.Apply.String(),
	})
	return nil
}

// InsertBuyerCreditAllocation menulis baris sisa tak-berjadwal (schedule NULL) —
// buyer_credit ATAU direct, mengikuti in.Type (meniru insertBuyerCreditTx nyata).
func (m *mockContractStore) InsertBuyerCreditAllocation(_ context.Context, tenantID uint64, in sale.BuyerCreditAllocationInput) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	allocType := in.Type
	if allocType == "" {
		allocType = sale.AllocationTypeBuyerCredit
	}
	m.allocations = append(m.allocations, recordedAllocation{
		TerminPaymentID: in.TerminPaymentID,
		ScheduleID:      nil,
		Type:            allocType,
		Amount:          in.Amount.String(),
	})
	return nil
}

// ListAllocationsByUnit/ByTermin — reader P4 (mock in-memory dari recorded rows).
func (m *mockContractStore) ListAllocationsByUnit(_ context.Context, tenantID, unitID uint64) ([]sale.AllocationView, error) {
	return nil, nil
}

func (m *mockContractStore) ListAllocationsByTermin(_ context.Context, tenantID, terminID uint64) ([]sale.AllocationView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []sale.AllocationView
	for _, a := range m.allocations {
		if a.TerminPaymentID != terminID {
			continue
		}
		out = append(out, sale.AllocationView{
			TerminPaymentID:   a.TerminPaymentID,
			PaymentScheduleID: a.ScheduleID,
			AllocationType:    string(a.Type),
			Amount:            a.Amount,
		})
	}
	return out, nil
}

// GetBuyerCredit (S8): meniru creditBalance kanonik — Σ alokasi buyer_credit
// yang terekam (mock tidak melacak credit_applications; applied selalu 0).
func (m *mockContractStore) GetBuyerCredit(_ context.Context, tenantID, unitID uint64) (*sale.BuyerCreditView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := domain.Zero
	for _, a := range m.allocations {
		if a.Type != sale.AllocationTypeBuyerCredit {
			continue
		}
		if m2, err := domain.NewMoney(a.Amount); err == nil {
			total = total.Add(m2)
		}
	}
	return &sale.BuyerCreditView{UnitID: unitID, Available: total.String(), TotalSources: total.String(), TotalApplied: "0"}, nil
}

func (m *mockContractStore) ApplyCredit(_ context.Context, tenantID, contractID uint64, req sale.ApplyCreditRequest) (*sale.ApplyCreditResult, error) {
	return nil, sale.ErrNoCreditAvailable
}

// Allocations mengembalikan salinan seluruh baris alokasi (test helper).
func (m *mockContractStore) Allocations() []recordedAllocation {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]recordedAllocation, len(m.allocations))
	copy(out, m.allocations)
	return out
}

func (m *mockContractStore) ListDueInPeriod(_ context.Context, tenantID uint64, from, to time.Time) ([]*sale.PaymentSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*sale.PaymentSchedule
	for _, s := range m.schedules {
		if s.TenantID == tenantID && !s.DueDate.Before(from) && !s.DueDate.After(to) {
			cp := *s
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *mockContractStore) ListScheduledBefore(_ context.Context, tenantID uint64, before time.Time) ([]*sale.PaymentSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*sale.PaymentSchedule
	for _, s := range m.schedules {
		if s.TenantID == tenantID && s.Status == sale.ScheduleStatusScheduled && s.DueDate.Before(before) {
			cp := *s
			result = append(result, &cp)
		}
	}
	return result, nil
}

func (m *mockContractStore) FindContractByUnitID(_ context.Context, tenantID, unitID uint64) (*sale.SaleContract, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, c := range m.contracts {
		if c.TenantID == tenantID && c.UnitID == unitID {
			cp := *c
			return &cp, nil
		}
	}
	return nil, sale.ErrContractNotFound
}

func (m *mockContractStore) ListSchedulesByContract(_ context.Context, tenantID, contractID uint64) ([]*sale.PaymentSchedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var result []*sale.PaymentSchedule
	for _, s := range m.schedules {
		if s.TenantID == tenantID && s.SaleContractID == contractID {
			cp := *s
			result = append(result, &cp)
		}
	}
	return result, nil
}

// ── helper: buildServiceWithContracts ────────────────────────────────────────

func buildServiceWithContracts(
	units map[uint64]*sale.UnitSaleInfo,
	termins []*sale.TerminPayment,
	cs *mockContractStore,
) (*sale.Service, *mockContractStore, *mockTerminStore) {
	af := &mockAccountFinder{accounts: standardAccounts}
	jw := &mockJournalWriter{nextID: 0}
	ur := &mockUnitReader{units: units}
	ts := &mockTerminStore{termins: termins}
	cp := &mockUnitCostProvider{}
	bw := &mockBASTWriter{}
	svc := sale.NewService(af, jw, ur, ts, cp, bw, sale.WithContractStore(cs))
	return svc, cs, ts
}

// ── Tests: CreateContract ─────────────────────────────────────────────────────

func TestService_CreateContract_Valid(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	req := sale.CreateContractRequest{
		UnitID:       5,
		BuyerName:    "Budi Santoso",
		BuyerID:      "3174012345670001",
		PaymentType:  sale.PaymentTypeKPR,
		ContractDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		TotalPrice:   rupiah(2_800_000_000),
	}
	contract, err := svc.CreateContract(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if contract.ID == 0 {
		t.Error("contract ID harus diisi setelah simpan")
	}
	if contract.UnitID != 5 {
		t.Errorf("UnitID: got %d, want 5", contract.UnitID)
	}
	if contract.PaymentType != sale.PaymentTypeKPR {
		t.Errorf("PaymentType: got %s, want kpr", contract.PaymentType)
	}
	if !contract.TotalPrice.Equal(rupiah(2_800_000_000)) {
		t.Errorf("TotalPrice: got %s, want 2800000000", contract.TotalPrice)
	}
}

func TestService_CreateContract_TotalPriceZero_ReturnsError(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	req := sale.CreateContractRequest{UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: domain.Zero}
	_, err := svc.CreateContract(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrContractTotalPriceInvalid) {
		t.Errorf("expected ErrContractTotalPriceInvalid, got %v", err)
	}
}

func TestService_CreateContract_UnitIDZero_ReturnsError(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	req := sale.CreateContractRequest{UnitID: 0, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(1_000_000_000)}
	_, err := svc.CreateContract(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrUnitRequired) {
		t.Errorf("expected ErrUnitRequired, got %v", err)
	}
}

func TestService_CreateContract_InvalidPaymentType_ReturnsError(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	req := sale.CreateContractRequest{UnitID: 5, PaymentType: "kredit", TotalPrice: rupiah(1_000_000_000)}
	_, err := svc.CreateContract(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrInvalidPaymentType) {
		t.Errorf("expected ErrInvalidPaymentType, got %v", err)
	}
}

// fakeUnitTransitioner mensimulasikan project.Service.Transition dgn matriks
// status yang sama (available→reserved→ppjb dst) tanpa DB — dipakai untuk
// menguji proyeksi lifecycle unit (D3) dari CreateContract secara terisolasi.
type fakeUnitTransitioner struct {
	mu     sync.Mutex
	status project.UnitStatus
	calls  []project.TransitionRequest
}

func newFakeUnitTransitioner(initial project.UnitStatus) *fakeUnitTransitioner {
	return &fakeUnitTransitioner{status: initial}
}

func (f *fakeUnitTransitioner) Transition(_ context.Context, _, _ uint64, req project.TransitionRequest) (*project.Unit, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, req)
	if !f.status.CanTransitionTo(req.Next) {
		return nil, fmt.Errorf("transisi tidak sah: %s -> %s", f.status, req.Next)
	}
	f.status = req.Next
	return &project.Unit{Status: f.status}, nil
}

// TestService_CreateContract_DirectFromAvailable_ProjectsUnitToPPJB adalah
// regression test untuk bug: jalur "Buat Kontrak" langsung (tanpa BookingID)
// berangkat dari unit status "available", tapi matriks status TIDAK
// mengizinkan available -> ppjb secara langsung (hanya reserved -> ppjb).
// Sebelum fix, transisi tunggal ke ppjb gagal (no-op silent) dan unit macet
// di "available" selamanya, memblokir Akad (butuh reserved/ppjb). Fix: D3
// mendahulukan available -> reserved (event reservation_confirmed, matriks
// "shortcut cash") sebelum reserved -> ppjb (event contract_signed).
func TestService_CreateContract_DirectFromAvailable_ProjectsUnitToPPJB(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)
	ft := newFakeUnitTransitioner(project.UnitStatusAvailable)
	svc.SetUnitTransitioner(ft)

	req := sale.CreateContractRequest{
		UnitID:       5,
		BuyerName:    "Budi Santoso",
		PaymentType:  sale.PaymentTypeKPR,
		ContractDate: time.Date(2024, 1, 15, 0, 0, 0, 0, time.UTC),
		TotalPrice:   rupiah(450_000_000),
	}
	if _, err := svc.CreateContract(context.Background(), 1, req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ft.mu.Lock()
	defer ft.mu.Unlock()
	if ft.status != project.UnitStatusPPJB {
		t.Fatalf("unit status akhir: got %s, want ppjb (unit tidak boleh macet di available)", ft.status)
	}
	if len(ft.calls) != 2 {
		t.Fatalf("jumlah panggilan Transition: got %d, want 2 (available->reserved, reserved->ppjb)", len(ft.calls))
	}
	if ft.calls[0].Next != project.UnitStatusReserved || ft.calls[0].Event != project.EventReservationConfirmed {
		t.Errorf("panggilan pertama: got (%s,%s), want (reserved,reservation_confirmed)", ft.calls[0].Next, ft.calls[0].Event)
	}
	if ft.calls[1].Next != project.UnitStatusPPJB || ft.calls[1].Event != project.EventContractSigned {
		t.Errorf("panggilan kedua: got (%s,%s), want (ppjb,contract_signed)", ft.calls[1].Next, ft.calls[1].Event)
	}
}

// ── Tests: CreatePaymentSchedule ─────────────────────────────────────────────

func TestService_CreatePaymentSchedule_Valid(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:      5,
		PaymentType: sale.PaymentTypeKPR,
		TotalPrice:  rupiah(2_800_000_000),
	})

	items := []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), Amount: rupiah(560_000_000), Type: sale.ScheduleTypeDP},
		{InstallmentNumber: 2, DueDate: time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC), Amount: rupiah(200_000_000), Type: sale.ScheduleTypeInstallment},
		{InstallmentNumber: 3, DueDate: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC), Amount: rupiah(2_040_000_000), Type: sale.ScheduleTypeFinal},
	}

	schedules, err := svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, items)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(schedules) != 3 {
		t.Errorf("jumlah baris jadwal: got %d, want 3", len(schedules))
	}
	for _, s := range schedules {
		if s.ID == 0 {
			t.Error("jadwal ID harus diisi")
		}
		if s.Status != sale.ScheduleStatusScheduled {
			t.Errorf("status awal harus scheduled, got %s", s.Status)
		}
		if s.UnitID != 5 {
			t.Errorf("UnitID jadwal harus 5, got %d", s.UnitID)
		}
	}
}

// ── Tests: MarkOverdue ────────────────────────────────────────────────────────

// TestService_MarkOverdue_Deterministic_WithInjectedTime membuktikan bahwa
// MarkOverdue TIDAK menggunakan time.Now() langsung — parameter now diinjektabel.
func TestService_MarkOverdue_Deterministic_WithInjectedTime(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:      5,
		PaymentType: sale.PaymentTypeTunai,
		TotalPrice:  rupiah(1_000_000_000),
	})

	cutoff := time.Date(2024, 6, 1, 0, 0, 0, 0, time.UTC)

	// Item A: jatuh tempo sebelum cutoff → overdue
	// Item B: jatuh tempo setelah cutoff → tetap scheduled
	items := []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: cutoff.Add(-24 * time.Hour), Amount: rupiah(300_000_000), Type: sale.ScheduleTypeDP},
		{InstallmentNumber: 2, DueDate: cutoff.Add(24 * time.Hour), Amount: rupiah(700_000_000), Type: sale.ScheduleTypeFinal},
	}
	schedules, _ := svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, items)

	// Gunakan cutoff deterministik — BUKAN time.Now()
	count, err := svc.MarkOverdue(context.Background(), 1, cutoff)
	if err != nil {
		t.Fatalf("MarkOverdue: %v", err)
	}
	if count != 1 {
		t.Errorf("MarkOverdue count: got %d, want 1 (hanya item A)", count)
	}

	// Item A harus overdue
	updated, err := cs.FindScheduleByID(context.Background(), 1, schedules[0].ID)
	if err != nil {
		t.Fatalf("FindScheduleByID: %v", err)
	}
	if updated.Status != sale.ScheduleStatusOverdue {
		t.Errorf("item A status: got %s, want overdue", updated.Status)
	}

	// Item B harus tetap scheduled
	unchanged, _ := cs.FindScheduleByID(context.Background(), 1, schedules[1].ID)
	if unchanged.Status != sale.ScheduleStatusScheduled {
		t.Errorf("item B harus tetap scheduled, got %s", unchanged.Status)
	}
}

func TestService_MarkOverdue_DoesNotMarkFutureItems(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:      5,
		PaymentType: sale.PaymentTypeTunai,
		TotalPrice:  rupiah(1_000_000_000),
	})

	now := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	items := []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: now.Add(30 * 24 * time.Hour), Amount: rupiah(1_000_000_000), Type: sale.ScheduleTypeFinal},
	}
	svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, items)

	count, err := svc.MarkOverdue(context.Background(), 1, now)
	if err != nil {
		t.Fatalf("MarkOverdue: %v", err)
	}
	if count != 0 {
		t.Errorf("tidak boleh ada overdue untuk item masa depan, got count=%d", count)
	}
}

// ── Tests: RecordInstallmentPaid ─────────────────────────────────────────────

// TestService_RecordInstallmentPaid_UsesEvent2_ThenMarksReceived membuktikan bahwa
// RecordInstallmentPaid LEWAT jalur Event 2 (RecordTermin) dan TIDAK posting jurnal baru sendiri.
func TestService_RecordInstallmentPaid_UsesEvent2_ThenMarksReceived(t *testing.T) {
	cs := newMockContractStore()
	units := map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}
	svc, _, ts := buildServiceWithContracts(units, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:      5,
		PaymentType: sale.PaymentTypeKPR,
		TotalPrice:  rupiah(2_800_000_000),
	})

	items := []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), Amount: rupiah(560_000_000), Type: sale.ScheduleTypeDP},
	}
	schedules, _ := svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, items)

	req := sale.RecordInstallmentPaidRequest{
		ScheduleID:      schedules[0].ID,
		BankAccountCode: "1-1300",
		ReceivedAt:      time.Date(2024, 2, 1, 10, 0, 0, 0, time.UTC),
		Description:     "DP cicilan 1",
	}
	updated, err := svc.RecordInstallmentPaid(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("RecordInstallmentPaid: %v", err)
	}

	// Status harus received
	if updated.Status != sale.ScheduleStatusReceived {
		t.Errorf("status: got %s, want received", updated.Status)
	}
	if updated.TerminPaymentID == nil {
		t.Error("TerminPaymentID harus diisi setelah diterima")
	}

	// Termin disimpan via RecordTermin (Event 2)
	if len(ts.termins) != 1 {
		t.Errorf("harus ada 1 termin (Event 2), got %d", len(ts.termins))
	}
	if !ts.termins[0].Amount.Equal(rupiah(560_000_000)) {
		t.Errorf("termin amount: got %s, want 560000000", ts.termins[0].Amount)
	}
}

func TestService_RecordInstallmentPaid_AlreadyReceived_ReturnsError(t *testing.T) {
	cs := newMockContractStore()
	units := map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}
	svc, _, _ := buildServiceWithContracts(units, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(1_000_000_000),
	})
	schedules, _ := svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: time.Now(), Amount: rupiah(1_000_000_000), Type: sale.ScheduleTypeFinal},
	})

	req := sale.RecordInstallmentPaidRequest{
		ScheduleID: schedules[0].ID, BankAccountCode: "1-1300", ReceivedAt: time.Now(),
	}
	_, _ = svc.RecordInstallmentPaid(context.Background(), 1, req)
	// Coba lagi — harus error
	_, err := svc.RecordInstallmentPaid(context.Background(), 1, req)
	if !errors.Is(err, sale.ErrInstallmentAlreadyReceived) {
		t.Errorf("expected ErrInstallmentAlreadyReceived, got %v", err)
	}
}

// ── Tests: BuyerRemainingBalance ─────────────────────────────────────────────

func TestService_BuyerRemainingBalance_BeforeAnyPayment(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(1_000_000_000),
	})

	remaining, err := svc.BuyerRemainingBalance(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("BuyerRemainingBalance: %v", err)
	}
	if !remaining.Equal(rupiah(1_000_000_000)) {
		t.Errorf("remaining: got %s, want 1000000000", remaining)
	}
}

func TestService_BuyerRemainingBalance_AfterPartialPayment(t *testing.T) {
	cs := newMockContractStore()
	existingTermins := []*sale.TerminPayment{
		{UnitID: 5, Amount: rupiah(300_000_000)},
		{UnitID: 5, Amount: rupiah(200_000_000)},
	}
	units := map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}
	svc, _, _ := buildServiceWithContracts(units, existingTermins, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(1_000_000_000),
	})

	remaining, err := svc.BuyerRemainingBalance(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("BuyerRemainingBalance: %v", err)
	}
	// 1000M - 300M - 200M = 500M
	if !remaining.Equal(rupiah(500_000_000)) {
		t.Errorf("remaining: got %s, want 500000000 (1000M - 500M terbayar)", remaining)
	}
}

// ── Tests: ListDueInPeriod ────────────────────────────────────────────────────

func TestService_ListDueInPeriod_ReturnsItemsInRange(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(3_000_000_000),
	})

	from := time.Date(2024, 3, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2024, 3, 31, 0, 0, 0, 0, time.UTC)
	items := []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: from.Add(-1 * time.Hour), Amount: rupiah(1_000_000_000), Type: sale.ScheduleTypeDP},       // sebelum range
		{InstallmentNumber: 2, DueDate: from, Amount: rupiah(1_000_000_000), Type: sale.ScheduleTypeInstallment},                    // batas awal (inklusif)
		{InstallmentNumber: 3, DueDate: to, Amount: rupiah(1_000_000_000), Type: sale.ScheduleTypeFinal},                           // batas akhir (inklusif)
	}
	svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, items)

	due, err := svc.ListDueInPeriod(context.Background(), 1, from, to)
	if err != nil {
		t.Fatalf("ListDueInPeriod: %v", err)
	}
	if len(due) != 2 {
		t.Errorf("items dalam period: got %d, want 2", len(due))
	}
}

// ── Tests: cross-tenant isolation ────────────────────────────────────────────

func TestService_ContractStore_CrossTenantIsolation(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(1_000_000_000),
	})

	// Tenant 2 coba akses kontrak tenant 1
	_, err := cs.FindContractByID(context.Background(), 2, contract.ID)
	if !errors.Is(err, sale.ErrContractNotFound) {
		t.Errorf("cross-tenant: expected ErrContractNotFound, got %v", err)
	}
}

// ── Tests: siklus KPR penuh ───────────────────────────────────────────────────

// TestService_FullCycleKPR_ContractScheduleTerminBASTBalance memverifikasi siklus
// penuh KPR: kontrak → jadwal → bayar DP → sisa hutang → BAST PKP → ledger balanced.
func TestService_FullCycleKPR_ContractScheduleTerminBASTBalance(t *testing.T) {
	cs := newMockContractStore()
	units := map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")}
	bastCapture := &mockBASTWriter{}
	af := &mockAccountFinder{accounts: standardAccounts}
	jw := &mockJournalWriter{}
	ur := &mockUnitReader{units: units}
	ts := &mockTerminStore{}
	cp := &mockUnitCostProvider{costs: map[uint64]domain.UnitCostBreakdown{
		5: {Hard: rupiah(1_000_000_000)},
	}}

	svc := sale.NewService(af, jw, ur, ts, cp, bastCapture, sale.WithContractStore(cs))

	// 1. Buat kontrak KPR
	bankKPR := "Bank BTN"
	loanAmt := rupiah(2_000_000_000)
	contract, err := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:       5,
		BuyerName:    "Sinta Dewi",
		BuyerID:      "3172056789012345",
		PaymentType:  sale.PaymentTypeKPR,
		BankKPR:      &bankKPR,
		LoanAmount:   &loanAmt,
		TotalPrice:   rupiah(2_800_000_000),
		ContractDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateContract: %v", err)
	}

	// 2. Jadwal: DP 800M + KPR 2B
	schedules, err := svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC), Amount: rupiah(800_000_000), Type: sale.ScheduleTypeDP},
		{InstallmentNumber: 2, DueDate: time.Date(2024, 8, 1, 0, 0, 0, 0, time.UTC), Amount: rupiah(2_000_000_000), Type: sale.ScheduleTypeFinal},
	})
	if err != nil {
		t.Fatalf("CreatePaymentSchedule: %v", err)
	}

	// 3. Bayar DP via Event 2
	_, err = svc.RecordInstallmentPaid(context.Background(), 1, sale.RecordInstallmentPaidRequest{
		ScheduleID:      schedules[0].ID,
		BankAccountCode: "1-1300",
		ReceivedAt:      time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		Description:     "DP KPR Sinta Dewi",
	})
	if err != nil {
		t.Fatalf("RecordInstallmentPaid DP: %v", err)
	}

	// 4. Total terkumpul = 800M
	collected, _ := svc.TotalCollectedByUnit(context.Background(), 1, 5)
	if !collected.Equal(rupiah(800_000_000)) {
		t.Errorf("total terkumpul: got %s, want 800000000", collected)
	}

	// 5. Sisa hutang = 2800M - 800M = 2000M
	remaining, _ := svc.BuyerRemainingBalance(context.Background(), 1, contract.ID)
	if !remaining.Equal(rupiah(2_000_000_000)) {
		t.Errorf("sisa hutang: got %s, want 2000000000", remaining)
	}

	// 6. BAST PKP — DPP 2.8B, PPN 11% = 308M
	_, err = svc.RecordAkad(context.Background(), 1, sale.RecordBASTRequest{
		UnitID:    5,
		SalePrice: rupiah(2_800_000_000),
		IsVAT:     true,
		VATRate:   decimal.NewFromFloat(0.11),
		BuyerRef:  "LITHOS-A01",
		BASTDate:  time.Date(2024, 8, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordBAST PKP: %v", err)
	}

	// 7. Event 3 balanced
	revLines := bastCapture.captured.RevenueLines
	assertBalanced(t, "BAST-PKP-Event3", revLines)
	assertWholeRupiah(t, "BAST-PKP-Event3", revLines)

	crPPN := sumCreditByAccount(revLines, accPPNKeluar)
	if !crPPN.Equal(rupiah(308_000_000)) {
		t.Errorf("Cr PPN: got %s, want 308000000 (2800M × 11%%)", crPPN)
	}
	crPendapatan := sumCreditByAccount(revLines, accPendapatan)
	if !crPendapatan.Equal(rupiah(2_800_000_000)) {
		t.Errorf("Cr Pendapatan: got %s, want 2800000000", crPendapatan)
	}
	drUMP := sumDebitByAccount(revLines, accUMP)
	if !drUMP.Equal(rupiah(800_000_000)) {
		t.Errorf("Dr UMP: got %s, want 800000000", drUMP)
	}
	drPiutang := sumDebitByAccount(revLines, accPiutang)
	if !drPiutang.Equal(rupiah(2_308_000_000)) {
		t.Errorf("Dr Piutang: got %s, want 2308000000 (3108M - 800M)", drPiutang)
	}

	// 8. Event 4 HPP (Invariant #4)
	cogsLines := bastCapture.captured.COGSLines
	assertBalanced(t, "BAST-PKP-Event4", cogsLines)
	drHPP := sumDebitByAccount(cogsLines, accHPP)
	if !drHPP.Equal(rupiah(1_000_000_000)) {
		t.Errorf("Dr HPP: got %s, want 1000000000", drHPP)
	}
}

// ── Tests: PKP 1.110M canonical (posting-rules Contoh B) ─────────────────────

func TestService_BAST_PKP_Canonical1110M_Balanced(t *testing.T) {
	existing := []*sale.TerminPayment{{UnitID: 5, Amount: rupiah(800_000_000)}}
	svc, bast, _ := buildService(standardAccounts,
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		existing,
		map[uint64]domain.UnitCostBreakdown{5: {Hard: rupiah(650_000_000)}},
		nil,
	)

	req := sale.RecordBASTRequest{
		UnitID:    5,
		SalePrice: rupiah(1_000_000_000),
		IsVAT:     true,
		VATRate:   decimal.NewFromFloat(0.11),
		BuyerRef:  "VILLA-05",
		BASTDate:  time.Now(),
	}
	_, err := svc.RecordAkad(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	lines := bast.captured.RevenueLines
	assertBalanced(t, "1110M-PKP-Event3", lines)
	assertWholeRupiah(t, "1110M-PKP-Event3", lines)

	d, _ := sumLines(lines)
	if !d.Equal(rupiah(1_110_000_000)) {
		t.Errorf("Σ debit Event3 PKP: got %s, want 1110000000", d)
	}

	crPPN := sumCreditByAccount(lines, accPPNKeluar)
	if !crPPN.Equal(rupiah(110_000_000)) {
		t.Errorf("Cr PPN: got %s, want 110000000", crPPN)
	}
	crPendapatan := sumCreditByAccount(lines, accPendapatan)
	if !crPendapatan.Equal(rupiah(1_000_000_000)) {
		t.Errorf("Cr Pendapatan (DPP): got %s, want 1000000000", crPendapatan)
	}
	drPiutang := sumDebitByAccount(lines, accPiutang)
	if !drPiutang.Equal(rupiah(310_000_000)) {
		t.Errorf("Dr Piutang (bruto): got %s, want 310000000", drPiutang)
	}
}

func TestService_BAST_NonPKP_Regresi_Tidak_Berubah(t *testing.T) {
	existing := []*sale.TerminPayment{{UnitID: 5, Amount: rupiah(800_000_000)}}
	svc, bast, _ := buildService(standardAccounts,
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		existing,
		map[uint64]domain.UnitCostBreakdown{5: {Hard: rupiah(650_000_000)}},
		nil,
	)

	_, err := svc.RecordAkad(context.Background(), 1, sale.RecordBASTRequest{
		UnitID: 5, SalePrice: rupiah(1_000_000_000), IsVAT: false, BASTDate: time.Now(),
	})
	if err != nil {
		t.Fatalf("RecordBAST non-PKP: %v", err)
	}

	lines := bast.captured.RevenueLines
	assertBalanced(t, "NonPKP-regresi", lines)

	crPPN := sumCreditByAccount(lines, accPPNKeluar)
	if !crPPN.IsZero() {
		t.Errorf("non-PKP: Cr PPN harus 0, got %s", crPPN)
	}
	drPiutang := sumDebitByAccount(lines, accPiutang)
	if !drPiutang.Equal(rupiah(200_000_000)) {
		t.Errorf("non-PKP: Dr Piutang: got %s, want 200000000", drPiutang)
	}
}

// ── Tests: PKP — gross_amount vs dpp_amount ───────────────────────────────────

// DoD: PKP → gross = DPP × (1 + vatRate); TotalPrice = gross; DPPAmount = DPP.
func TestService_CreateContract_PKP_GrossAmountCalculated(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	// DPP 2.800M, tarif PPN 11% → gross = 2.800M × 1.11 = 3.108M
	req := sale.CreateContractRequest{
		UnitID:          5,
		PaymentType:     sale.PaymentTypeTunai,
		TotalPrice:      rupiah(2_800_000_000), // DPP
		IsPKP:           true,
		VATRateSnapshot: decimal.NewFromFloat(0.11),
	}
	contract, err := svc.CreateContract(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantGross := rupiah(3_108_000_000)
	wantDPP := rupiah(2_800_000_000)

	if !contract.GrossAmount.Equal(wantGross) {
		t.Errorf("GrossAmount: got %s, want %s (2800M × 1.11)", contract.GrossAmount, wantGross)
	}
	if !contract.DPPAmount.Equal(wantDPP) {
		t.Errorf("DPPAmount: got %s, want %s", contract.DPPAmount, wantDPP)
	}
	// TotalPrice adalah alias GrossAmount
	if !contract.TotalPrice.Equal(wantGross) {
		t.Errorf("TotalPrice (alias gross): got %s, want %s", contract.TotalPrice, wantGross)
	}
	if !contract.IsPKP {
		t.Error("IsPKP harus true")
	}
	if !contract.VATRateSnapshot.Equal(decimal.NewFromFloat(0.11)) {
		t.Errorf("VATRateSnapshot: got %s, want 0.11", contract.VATRateSnapshot)
	}
}

// DoD: non-PKP → GrossAmount == DPP; TotalPrice tidak berubah.
func TestService_CreateContract_NonPKP_GrossEqualsDPP(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	req := sale.CreateContractRequest{
		UnitID:      5,
		PaymentType: sale.PaymentTypeTunai,
		TotalPrice:  rupiah(1_000_000_000), // DPP = Gross untuk non-PKP
	}
	contract, err := svc.CreateContract(context.Background(), 1, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := rupiah(1_000_000_000)
	if !contract.GrossAmount.Equal(want) {
		t.Errorf("NonPKP GrossAmount: got %s, want %s", contract.GrossAmount, want)
	}
	if !contract.DPPAmount.Equal(want) {
		t.Errorf("NonPKP DPPAmount: got %s, want %s", contract.DPPAmount, want)
	}
	if !contract.TotalPrice.Equal(want) {
		t.Errorf("NonPKP TotalPrice: got %s, want %s", contract.TotalPrice, want)
	}
	if contract.IsPKP {
		t.Error("non-PKP: IsPKP harus false")
	}
}

// DoD: BuyerRemainingBalance untuk PKP menggunakan gross_amount (bruto).
// DPP 2.800M, PPN 11% → gross 3.108M; collected 800M → sisa 2.308M.
func TestService_BuyerRemainingBalance_PKP_UsesGross(t *testing.T) {
	cs := newMockContractStore()
	existingTermins := []*sale.TerminPayment{
		{UnitID: 5, Amount: rupiah(800_000_000)},
	}
	svc, _, _ := buildServiceWithContracts(
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		existingTermins,
		cs,
	)

	contract, err := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:          5,
		PaymentType:     sale.PaymentTypeTunai,
		TotalPrice:      rupiah(2_800_000_000), // DPP
		IsPKP:           true,
		VATRateSnapshot: decimal.NewFromFloat(0.11),
	})
	if err != nil {
		t.Fatalf("CreateContract PKP: %v", err)
	}

	// Verifikasi gross tersimpan dengan benar
	if !contract.GrossAmount.Equal(rupiah(3_108_000_000)) {
		t.Fatalf("gross: got %s, want 3108000000", contract.GrossAmount)
	}

	remaining, err := svc.BuyerRemainingBalance(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("BuyerRemainingBalance: %v", err)
	}

	// 3.108M (gross) − 800M (terbayar) = 2.308M
	want := rupiah(2_308_000_000)
	if !remaining.Equal(want) {
		t.Errorf("sisa hutang PKP: got %s, want %s (3108M − 800M)", remaining, want)
	}
}

// DoD: non-PKP tidak berubah — BuyerRemainingBalance pakai DPP sama persis.
func TestService_BuyerRemainingBalance_NonPKP_Unchanged(t *testing.T) {
	cs := newMockContractStore()
	existingTermins := []*sale.TerminPayment{
		{UnitID: 5, Amount: rupiah(300_000_000)},
		{UnitID: 5, Amount: rupiah(200_000_000)},
	}
	svc, _, _ := buildServiceWithContracts(
		map[uint64]*sale.UnitSaleInfo{5: defaultUnit(5, "reserved")},
		existingTermins,
		cs,
	)

	contract, err := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:      5,
		PaymentType: sale.PaymentTypeTunai,
		TotalPrice:  rupiah(1_000_000_000), // DPP = Gross
	})
	if err != nil {
		t.Fatalf("CreateContract non-PKP: %v", err)
	}

	remaining, err := svc.BuyerRemainingBalance(context.Background(), 1, contract.ID)
	if err != nil {
		t.Fatalf("BuyerRemainingBalance: %v", err)
	}

	// 1000M − 500M = 500M — sama persis dengan perilaku sebelumnya
	want := rupiah(500_000_000)
	if !remaining.Equal(want) {
		t.Errorf("sisa hutang non-PKP: got %s, want %s", remaining, want)
	}
}

// DoD: Σ payment schedule harus == GrossAmount kontrak (bukan DPP).
func TestService_ScheduleSum_MustEqualGrossAmount(t *testing.T) {
	cs := newMockContractStore()
	svc, _, _ := buildServiceWithContracts(nil, nil, cs)

	// PKP: DPP 3M, PPN 11% → gross 3.33M
	contract, _ := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID:          5,
		PaymentType:     sale.PaymentTypeKPR,
		TotalPrice:      rupiah(3_000_000_000),
		IsPKP:           true,
		VATRateSnapshot: decimal.NewFromFloat(0.11),
	})

	gross := contract.GrossAmount // 3.33M = 3_330_000_000

	// Jadwal: 3 cicilan yang totalnya == gross
	dp := gross.Decimal().Mul(decimal.NewFromFloat(0.2)).Round(0)     // 20%
	sisanya := gross.Decimal().Sub(dp)                                 // 80%
	items := []sale.ScheduleItem{
		{InstallmentNumber: 1, DueDate: time.Now().Add(30 * 24 * time.Hour), Amount: domain.FromDecimal(dp), Type: sale.ScheduleTypeDP},
		{InstallmentNumber: 2, DueDate: time.Now().Add(60 * 24 * time.Hour), Amount: domain.FromDecimal(sisanya), Type: sale.ScheduleTypeFinal},
	}
	schedules, err := svc.CreatePaymentSchedule(context.Background(), 1, contract.ID, items)
	if err != nil {
		t.Fatalf("CreatePaymentSchedule: %v", err)
	}

	var total domain.Money
	for _, s := range schedules {
		total = total.Add(s.Amount)
	}
	if !total.Equal(gross) {
		t.Errorf("Σ schedule (%s) != gross_amount (%s)", total, gross)
	}
}

// ── Tests: ContractStore wajib dikonfigurasi ──────────────────────────────────

func TestService_CreateContract_WithoutContractStore_ReturnsError(t *testing.T) {
	svc := sale.NewService(
		&mockAccountFinder{accounts: standardAccounts},
		&mockJournalWriter{},
		&mockUnitReader{},
		&mockTerminStore{},
		&mockUnitCostProvider{},
		&mockBASTWriter{},
	)
	_, err := svc.CreateContract(context.Background(), 1, sale.CreateContractRequest{
		UnitID: 5, PaymentType: sale.PaymentTypeTunai, TotalPrice: rupiah(1_000_000_000),
	})
	if !errors.Is(err, sale.ErrContractStoreNotConfigured) {
		t.Errorf("expected ErrContractStoreNotConfigured, got %v", err)
	}
}
