package allocation_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
)

// ── Mocks ─────────────────────────────────────────────────────────────────────

type mockConfigStore struct {
	config *allocation.AllocationConfig
	saveErr error
	getErr  error
}

func (m *mockConfigStore) GetConfig(ctx context.Context, tenantID, projectID uint64) (*allocation.AllocationConfig, error) {
	if m.getErr != nil {
		return nil, m.getErr
	}
	return m.config, nil
}

func (m *mockConfigStore) SaveConfig(ctx context.Context, cfg *allocation.AllocationConfig) error {
	if m.saveErr != nil {
		return m.saveErr
	}
	m.config = cfg
	return nil
}

type mockDirectCostProvider struct {
	breakdown domain.UnitCostBreakdown
	err       error
}

func (m *mockDirectCostProvider) GetProjectWideCosts(ctx context.Context, tenantID, projectID uint64) (domain.UnitCostBreakdown, error) {
	return m.breakdown, m.err
}

func (m *mockDirectCostProvider) GetUnitDirectCosts(ctx context.Context, tenantID, unitID uint64) (domain.UnitCostBreakdown, error) {
	return m.breakdown, m.err
}

type mockUnitAttributeSource struct {
	units []allocation.UnitInput
	err   error
}

func (m *mockUnitAttributeSource) GetUnitInputs(ctx context.Context, tenantID, projectID uint64) ([]allocation.UnitInput, error) {
	return m.units, m.err
}

// ── Tests: SetBasis ───────────────────────────────────────────────────────────

func TestService_SetBasis_Valid(t *testing.T) {
	store := &mockConfigStore{}
	svc := allocation.NewService(store, nil, nil)

	err := svc.SetBasis(context.Background(), 1, 99, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.config == nil {
		t.Fatal("config not saved")
	}
	if store.config.Basis != allocation.BasisSaleableArea {
		t.Errorf("basis=%q, want saleable_area", store.config.Basis)
	}
	if store.config.TenantID != 1 {
		t.Errorf("tenantID=%d, want 1", store.config.TenantID)
	}
	if store.config.ProjectID != 99 {
		t.Errorf("projectID=%d, want 99", store.config.ProjectID)
	}
}

func TestService_SetBasis_Invalid(t *testing.T) {
	store := &mockConfigStore{}
	svc := allocation.NewService(store, nil, nil)
	err := svc.SetBasis(context.Background(), 1, 99, "invalid")
	if !errors.Is(err, allocation.ErrInvalidBasis) {
		t.Errorf("expected ErrInvalidBasis, got %v", err)
	}
}

func TestService_SetBasis_ZeroProject_ReturnsError(t *testing.T) {
	store := &mockConfigStore{}
	svc := allocation.NewService(store, nil, nil)
	err := svc.SetBasis(context.Background(), 1, 0, allocation.BasisSaleableArea)
	if !errors.Is(err, allocation.ErrProjectRequired) {
		t.Errorf("expected ErrProjectRequired, got %v", err)
	}
}

func TestService_SetBasis_UpdatesExisting(t *testing.T) {
	existing := &allocation.AllocationConfig{
		ID: 5, TenantID: 1, ProjectID: 42, Basis: allocation.BasisSaleableArea,
	}
	store := &mockConfigStore{config: existing, getErr: nil}
	svc := allocation.NewService(store, nil, nil)

	err := svc.SetBasis(context.Background(), 1, 42, allocation.BasisSalesValue)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if store.config.Basis != allocation.BasisSalesValue {
		t.Errorf("basis=%q, want sales_value", store.config.Basis)
	}
	if store.config.ID != 5 {
		t.Errorf("ID should be preserved: got %d, want 5", store.config.ID)
	}
}

func TestService_SetBasis_StoreError_Propagates(t *testing.T) {
	sentinel := errors.New("db down")
	store := &mockConfigStore{saveErr: sentinel}
	svc := allocation.NewService(store, nil, nil)
	err := svc.SetBasis(context.Background(), 1, 99, allocation.BasisSaleableArea)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected db sentinel, got %v", err)
	}
}

// ── Tests: GetBasis ───────────────────────────────────────────────────────────

func TestService_GetBasis_Found(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 7, Basis: allocation.BasisSalesValue,
	}}
	svc := allocation.NewService(store, nil, nil)
	cfg, err := svc.GetBasis(context.Background(), 1, 7)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Basis != allocation.BasisSalesValue {
		t.Errorf("basis=%q, want sales_value", cfg.Basis)
	}
}

func TestService_GetBasis_NotFound(t *testing.T) {
	store := &mockConfigStore{getErr: allocation.ErrConfigNotFound}
	svc := allocation.NewService(store, nil, nil)
	_, err := svc.GetBasis(context.Background(), 1, 7)
	if !errors.Is(err, allocation.ErrConfigNotFound) {
		t.Errorf("expected ErrConfigNotFound, got %v", err)
	}
}

// ── Tests: ComputeAllocation ──────────────────────────────────────────────────

func makeUnits() []allocation.UnitInput {
	return []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(150), SalesValue: rupiah(2_800_000_000)},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(200), SalesValue: rupiah(3_500_000_000)},
		{UnitID: 3, SaleableArea: decimal.NewFromInt(250), SalesValue: rupiah(4_200_000_000)},
	}
}

func TestService_ComputeAllocation_Success(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{
		Land:      rupiah(3_000_000_000),
		Hard:      rupiah(10_000_000_000),
		Soft:      rupiah(1_500_000_000),
		Financing: rupiah(750_000_000),
	}
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: projectWide}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	svc := allocation.NewService(store, costProvider, unitSource)

	results, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Reconciliation: Σ allocated == projectWide per category
	var sumLand, sumHard, sumSoft, sumFin domain.Money
	for _, r := range results {
		sumLand = sumLand.Add(r.Allocated.Land)
		sumHard = sumHard.Add(r.Allocated.Hard)
		sumSoft = sumSoft.Add(r.Allocated.Soft)
		sumFin = sumFin.Add(r.Allocated.Financing)
	}
	if !sumLand.Equal(projectWide.Land) {
		t.Errorf("land: Σallocated=%s, want %s", sumLand, projectWide.Land)
	}
	if !sumHard.Equal(projectWide.Hard) {
		t.Errorf("hard: Σallocated=%s, want %s", sumHard, projectWide.Hard)
	}
}

func TestService_ComputeAllocation_NoConfig_ReturnsError(t *testing.T) {
	store := &mockConfigStore{getErr: allocation.ErrConfigNotFound}
	svc := allocation.NewService(store, &mockDirectCostProvider{}, &mockUnitAttributeSource{})
	_, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if !errors.Is(err, allocation.ErrConfigNotFound) {
		t.Errorf("expected ErrConfigNotFound, got %v", err)
	}
}

func TestService_ComputeAllocation_NoUnits_ReturnsError(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 5, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{}
	unitSource := &mockUnitAttributeSource{units: nil}
	svc := allocation.NewService(store, costProvider, unitSource)
	_, err := svc.ComputeAllocation(context.Background(), 1, 5)
	if !errors.Is(err, allocation.ErrNoUnits) {
		t.Errorf("expected ErrNoUnits, got %v", err)
	}
}

func TestService_ComputeAllocation_AllWeightsZero_ReturnsError(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 5, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{
		breakdown: domain.UnitCostBreakdown{Hard: rupiah(1_000_000)},
	}
	unitSource := &mockUnitAttributeSource{units: []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.Zero},
		{UnitID: 2, SaleableArea: decimal.Zero},
	}}
	svc := allocation.NewService(store, costProvider, unitSource)
	_, err := svc.ComputeAllocation(context.Background(), 1, 5)
	if !errors.Is(err, allocation.ErrAllWeightsZero) {
		t.Errorf("expected ErrAllWeightsZero, got %v", err)
	}
}

func TestService_ComputeAllocation_ZeroProjectCosts_Success(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 5, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	svc := allocation.NewService(store, costProvider, unitSource)

	results, err := svc.ComputeAllocation(context.Background(), 1, 5)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for i, r := range results {
		if !r.Allocated.Total().IsZero() {
			t.Errorf("unit[%d] should have zero allocation, got %s", i, r.Allocated.Total())
		}
	}
}

func TestService_ComputeAllocation_UnitSourceError_Propagates(t *testing.T) {
	sentinel := errors.New("unit db error")
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 5, Basis: allocation.BasisSaleableArea,
	}}
	unitSource := &mockUnitAttributeSource{err: sentinel}
	costProvider := &mockDirectCostProvider{}
	svc := allocation.NewService(store, costProvider, unitSource)
	_, err := svc.ComputeAllocation(context.Background(), 1, 5)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel, got %v", err)
	}
}

func TestService_ComputeAllocation_CostProviderError_Propagates(t *testing.T) {
	sentinel := errors.New("cost db error")
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 5, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{err: sentinel}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	svc := allocation.NewService(store, costProvider, unitSource)
	_, err := svc.ComputeAllocation(context.Background(), 1, 5)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel, got %v", err)
	}
}

// ── Mocks: ExecutionStore, UserEmailFinder ────────────────────────────────────

type mockExecutionStore struct {
	mu   sync.Mutex
	rows []*allocation.AllocationExecution
	err  error
}

func (m *mockExecutionStore) SaveExecution(_ context.Context, e *allocation.AllocationExecution) error {
	if m.err != nil {
		return m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e.ID = uint64(len(m.rows) + 1)
	cp := *e
	m.rows = append(m.rows, &cp)
	return nil
}

func (m *mockExecutionStore) ListExecutions(_ context.Context, tenantID, projectID uint64) ([]*allocation.AllocationExecution, error) {
	if m.err != nil {
		return nil, m.err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*allocation.AllocationExecution
	for i := len(m.rows) - 1; i >= 0; i-- {
		if m.rows[i].TenantID == tenantID && m.rows[i].ProjectID == projectID {
			out = append(out, m.rows[i])
		}
	}
	return out, nil
}

type mockUserEmailFinder struct{ email string }

func (m *mockUserEmailFinder) FindUserEmail(_ context.Context, _, _ uint64) (string, error) {
	return m.email, nil
}

func makeAllocSvc(basis allocation.AllocationBasis) *allocation.Service {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		ID: 1, TenantID: 1, ProjectID: 10, Basis: basis,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{
		Land: rupiah(5_000_000_000),
		Hard: rupiah(8_000_000_000),
	}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	execStore := &mockExecutionStore{}
	emailFinder := &mockUserEmailFinder{email: "owner@test.com"}
	return allocation.NewService(store, costProvider, unitSource,
		allocation.WithExecutionStore(execStore),
		allocation.WithUserEmailFinder(emailFinder),
	)
}

// ── Tests: Execute ────────────────────────────────────────────────────────────

// QA-1: Eksekusi pertama → satu record tersimpan, snapshot valid.
func TestService_Execute_FirstTime_CreatesRecord(t *testing.T) {
	execStore := &mockExecutionStore{}
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		ID: 1, TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{
		Land: rupiah(5_000_000_000),
		Hard: rupiah(8_000_000_000),
	}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}

	svc := allocation.NewService(store, costProvider, unitSource,
		allocation.WithExecutionStore(execStore),
		allocation.WithUserEmailFinder(&mockUserEmailFinder{email: "owner@test.com"}),
	)

	exec, err := svc.Execute(context.Background(), 1, 10, 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exec == nil {
		t.Fatal("expected AllocationExecution, got nil")
	}
	if exec.Basis != allocation.BasisSaleableArea {
		t.Errorf("basis=%q, want saleable_area", exec.Basis)
	}
	if exec.UserEmail != "owner@test.com" {
		t.Errorf("user_email=%q, want owner@test.com", exec.UserEmail)
	}
	if exec.TotalCost.IsZero() {
		t.Error("total_cost should not be zero when project has costs")
	}
	if exec.TenantID != 1 || exec.ProjectID != 10 || exec.ExecutedBy != 99 {
		t.Errorf("snapshot fields wrong: tenant=%d project=%d user=%d", exec.TenantID, exec.ProjectID, exec.ExecutedBy)
	}
	if len(execStore.rows) != 1 {
		t.Errorf("expected 1 saved row, got %d", len(execStore.rows))
	}
}

// QA-2: Eksekusi ulang tanpa ubah config → record baru ditambahkan (append-only),
// bukan ditolak sebagai duplikat. Design: audit trail tak terbatas.
func TestService_Execute_Rerun_AppendsRecord(t *testing.T) {
	execStore := &mockExecutionStore{}
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		ID: 1, TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{
		Hard: rupiah(3_000_000_000),
	}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	svc := allocation.NewService(store, costProvider, unitSource,
		allocation.WithExecutionStore(execStore),
		allocation.WithUserEmailFinder(&mockUserEmailFinder{email: "owner@test.com"}),
	)

	for i := 0; i < 3; i++ {
		if _, err := svc.Execute(context.Background(), 1, 10, 1); err != nil {
			t.Fatalf("execute #%d: %v", i+1, err)
		}
	}
	if len(execStore.rows) != 3 {
		t.Errorf("expected 3 rows after 3 executions, got %d", len(execStore.rows))
	}
	// ListExecutions returns newest-first; version number = position from oldest
	list, err := svc.ListExecutions(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 3 {
		t.Errorf("history length: got %d, want 3", len(list))
	}
}

// QA-3: Ubah basis Area→Value, eksekusi masing-masing.
// History[oldest].basis == saleable_area, history[newest].basis == sales_value.
func TestService_Execute_BasisChange_HistoryReflectsCorrectBasis(t *testing.T) {
	execStore := &mockExecutionStore{}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{
		Land: rupiah(2_000_000_000),
	}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}

	cfgStore := &mockConfigStore{config: &allocation.AllocationConfig{
		ID: 1, TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	svc := allocation.NewService(cfgStore, costProvider, unitSource,
		allocation.WithExecutionStore(execStore),
		allocation.WithUserEmailFinder(&mockUserEmailFinder{email: "a@test.com"}),
	)

	// Execution 1: basis = saleable_area
	if _, err := svc.Execute(context.Background(), 1, 10, 1); err != nil {
		t.Fatalf("execute area: %v", err)
	}

	// Change basis to sales_value
	cfgStore.config.Basis = allocation.BasisSalesValue
	// Execution 2: basis = sales_value
	if _, err := svc.Execute(context.Background(), 1, 10, 1); err != nil {
		t.Fatalf("execute value: %v", err)
	}

	list, _ := svc.ListExecutions(context.Background(), 1, 10)
	if len(list) != 2 {
		t.Fatalf("expected 2 history entries, got %d", len(list))
	}
	// List is newest-first: list[0]=execution2 (sales_value), list[1]=execution1 (saleable_area)
	if list[0].Basis != allocation.BasisSalesValue {
		t.Errorf("newest entry: basis=%q, want sales_value", list[0].Basis)
	}
	if list[1].Basis != allocation.BasisSaleableArea {
		t.Errorf("oldest entry: basis=%q, want saleable_area", list[1].Basis)
	}
}

// QA-5 (service layer): ListExecutions menyaring berdasarkan tenant_id.
// Tenant 2 tidak bisa melihat eksekusi Tenant 1.
func TestService_ListExecutions_TenantIsolation(t *testing.T) {
	execStore := &mockExecutionStore{}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{
		Hard: rupiah(1_000_000_000),
	}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}

	// Tenant 1 melakukan eksekusi
	cfgTenant1 := &mockConfigStore{config: &allocation.AllocationConfig{
		ID: 1, TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	svcTenant1 := allocation.NewService(cfgTenant1, costProvider, unitSource,
		allocation.WithExecutionStore(execStore),
		allocation.WithUserEmailFinder(&mockUserEmailFinder{email: "t1@test.com"}),
	)
	if _, err := svcTenant1.Execute(context.Background(), 1, 10, 1); err != nil {
		t.Fatalf("tenant1 execute: %v", err)
	}
	if _, err := svcTenant1.Execute(context.Background(), 1, 10, 1); err != nil {
		t.Fatalf("tenant1 execute 2: %v", err)
	}

	// Tenant 2 menyetel config sendiri dan mengeksekusi
	cfgTenant2 := &mockConfigStore{config: &allocation.AllocationConfig{
		ID: 2, TenantID: 2, ProjectID: 10, Basis: allocation.BasisSalesValue,
	}}
	svcTenant2 := allocation.NewService(cfgTenant2, costProvider, unitSource,
		allocation.WithExecutionStore(execStore),
		allocation.WithUserEmailFinder(&mockUserEmailFinder{email: "t2@test.com"}),
	)
	if _, err := svcTenant2.Execute(context.Background(), 2, 10, 99); err != nil {
		t.Fatalf("tenant2 execute: %v", err)
	}

	// Tenant 2 hanya boleh melihat miliknya sendiri
	listT2, err := svcTenant2.ListExecutions(context.Background(), 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listT2) != 1 {
		t.Errorf("tenant2 should see 1 execution, got %d", len(listT2))
	}
	if listT2[0].TenantID != 2 {
		t.Errorf("tenant2 result has tenant_id=%d, want 2", listT2[0].TenantID)
	}

	// Tenant 1 tidak terpengaruh oleh eksekusi Tenant 2
	listT1, err := svcTenant1.ListExecutions(context.Background(), 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(listT1) != 2 {
		t.Errorf("tenant1 should see 2 executions, got %d", len(listT1))
	}
	for _, e := range listT1 {
		if e.TenantID != 1 {
			t.Errorf("tenant1 got row with tenant_id=%d — isolation violation", e.TenantID)
		}
	}
}

// Execute tanpa ExecStore dikonfigurasi → error jelas.
func TestService_Execute_NoExecStore_ReturnsError(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	svc := allocation.NewService(store, &mockDirectCostProvider{}, &mockUnitAttributeSource{units: makeUnits()})
	_, err := svc.Execute(context.Background(), 1, 10, 1)
	if err == nil {
		t.Fatal("expected error when executions store not configured")
	}
}
