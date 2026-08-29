package allocation_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
)

// ── Mocks ─────────────────────────────────────────────────────────────────────

type mockLandPoolSource struct {
	participants []allocation.LandPoolParticipant
	err          error
}

func (m *mockLandPoolSource) GetLandPoolParticipants(ctx context.Context, tenantID, projectID uint64) ([]allocation.LandPoolParticipant, error) {
	return m.participants, m.err
}

// ── Tests: ComputeLandPool (pure function) ──────────────────────────────────

// Σ(Allocated) == landPoolCost, persis — Invariant #3.
func TestComputeLandPool_ReconcilesToLastRupiah(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.NewFromInt(150)},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 7, LandAreaM2: decimal.NewFromInt(333)},
	}
	pool := rupiah(1_000_000_001) // ganjil, memaksa sisa pembulatan

	results := allocation.ComputeLandPool(pool, participants)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	var sum domain.Money
	for _, r := range results {
		sum = sum.Add(r.Allocated)
	}
	if !sum.Equal(pool) {
		t.Errorf("Σallocated=%s, want %s (exact reconciliation)", sum, pool)
	}
}

// Peserta dengan land_area nol tetap boleh ikut (mendapat porsi nol) — bukan
// kondisi galat, berbeda dari buildWeights di engine.go.
func TestComputeLandPool_ZeroAreaParticipant_GetsZeroShare(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.Zero},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 1, LandAreaM2: decimal.NewFromInt(500)},
	}
	results := allocation.ComputeLandPool(rupiah(10_000_000), participants)
	if !results[0].Allocated.IsZero() {
		t.Errorf("unit with zero land_area should get zero share, got %s", results[0].Allocated)
	}
	if results[1].Allocated.IsZero() {
		t.Error("land_stock should absorb the full pool when it's the only weighted participant")
	}
}

// Semua bobot nol (proyek belum backfill land_area sama sekali) → semua nol,
// TANPA error (berbeda dari engine.Compute/buildWeights yang punya
// ErrAllWeightsZero terpisah) — Money.Allocate menangani degenerate case ini
// secara native.
func TestComputeLandPool_AllWeightsZero_NoErrorAllZero(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.Zero},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 1, LandAreaM2: decimal.Zero},
	}
	results := allocation.ComputeLandPool(rupiah(5_000_000), participants)
	for i, r := range results {
		if !r.Allocated.IsZero() {
			t.Errorf("participant[%d] should be zero in degenerate case, got %s", i, r.Allocated)
		}
	}
}

// ── Tests: Service.ComputeAllocation / ComputeBudgeted — gating behavior ──────

// KRITIS: proyek TANPA land_stock (mayoritas portofolio) harus byte-identik
// sebelum/sesudah WithLandPoolSource dipasang — override adalah NO-OP.
func TestService_ComputeAllocation_NoLandStock_LandUnchanged(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{
		Land: rupiah(3_000_000_000),
		Hard: rupiah(10_000_000_000),
	}
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: projectWide}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}

	baseline := allocation.NewService(store, costProvider, unitSource)
	baselineResults, err := baseline.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("baseline: %v", err)
	}

	// Proyek tanpa land_stock: participants hanya berisi unit (tanpa land_stock kind).
	landPool := &mockLandPoolSource{participants: []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.Zero},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.Zero},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 3, LandAreaM2: decimal.Zero},
	}}
	withOverride := allocation.NewService(store, costProvider, unitSource, allocation.WithLandPoolSource(landPool))
	overrideResults, err := withOverride.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("with override: %v", err)
	}

	if len(baselineResults) != len(overrideResults) {
		t.Fatalf("result count mismatch: %d vs %d", len(baselineResults), len(overrideResults))
	}
	for i := range baselineResults {
		if !baselineResults[i].Total.Land.Equal(overrideResults[i].Total.Land) {
			t.Errorf("unit[%d] Land changed without land_stock present: baseline=%s, override=%s",
				i, baselineResults[i].Total.Land, overrideResults[i].Total.Land)
		}
		if !baselineResults[i].Allocated.Land.Equal(overrideResults[i].Allocated.Land) {
			t.Errorf("unit[%d] Allocated.Land changed without land_stock present", i)
		}
	}
}

// Proyek DENGAN land_stock: Land unit di-override oleh alokasi land_area-weighted,
// dan Σ(unit Land baru) + land_stock share == pool.Land persis.
func TestService_ComputeAllocation_WithLandStock_LandOverridden(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{
		Land: rupiah(3_000_000_000),
		Hard: rupiah(10_000_000_000),
	}
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: projectWide}
	unitSource := &mockUnitAttributeSource{units: makeUnits()} // UnitID 1,2,3

	landPool := &mockLandPoolSource{participants: []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.NewFromInt(150)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 3, LandAreaM2: decimal.NewFromInt(200)},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 9, LandAreaM2: decimal.NewFromInt(500)},
	}}
	svc := allocation.NewService(store, costProvider, unitSource, allocation.WithLandPoolSource(landPool))

	results, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Hard tetap saleable_area-weighted (tidak disentuh override).
	var sumHard, sumLand domain.Money
	for _, r := range results {
		sumHard = sumHard.Add(r.Allocated.Hard)
		sumLand = sumLand.Add(r.Allocated.Land)
	}
	if !sumHard.Equal(projectWide.Hard) {
		t.Errorf("hard: Σallocated=%s, want %s", sumHard, projectWide.Hard)
	}

	// Land unit-only sum harus STRICTLY LESS than pool.Land (land_stock menyerap sisanya) —
	// kecuali kasus degenerate mustahil di sini karena land_stock punya bobot > 0.
	if !sumLand.LessThan(projectWide.Land) {
		t.Errorf("Σunit Land=%s should be < pool.Land=%s (land_stock must absorb a share)", sumLand, projectWide.Land)
	}

	// Cross-check dengan ComputeLandPool langsung: unit shares harus identik.
	direct := allocation.ComputeLandPool(projectWide.Land, landPool.participants)
	directByUnit := map[uint64]domain.Money{}
	for _, d := range direct {
		if d.Kind == allocation.LandPoolParticipantUnit {
			directByUnit[d.UnitID] = d.Allocated
		}
	}
	for _, r := range results {
		want, ok := directByUnit[r.UnitID]
		if !ok {
			t.Fatalf("unit %d missing from direct computation", r.UnitID)
		}
		if !r.Allocated.Land.Equal(want) {
			t.Errorf("unit %d: Allocated.Land=%s, want %s (from direct ComputeLandPool)", r.UnitID, r.Allocated.Land, want)
		}
		if !r.Total.Land.Equal(r.Direct.Land.Add(want)) {
			t.Errorf("unit %d: Total.Land should equal Direct.Land + new Allocated.Land", r.UnitID)
		}
	}
}

// ComputeBudgeted juga digating dengan cara yang sama (jalur HPP saat Akad).
func TestService_ComputeBudgeted_WithLandStock_LandOverridden(t *testing.T) {
	pool := domain.UnitCostBreakdown{Land: rupiah(2_000_000_000)}
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	landPool := &mockLandPoolSource{participants: []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.NewFromInt(150)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 3, LandAreaM2: decimal.NewFromInt(250)},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 3, LandAreaM2: decimal.NewFromInt(1000)},
	}}
	svc := allocation.NewService(store, &mockDirectCostProvider{}, unitSource, allocation.WithLandPoolSource(landPool))

	results, basis, err := svc.ComputeBudgeted(context.Background(), 1, 10, pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if basis != allocation.BasisSaleableArea {
		t.Errorf("basis=%q, want saleable_area", basis)
	}
	var sumLand domain.Money
	for _, r := range results {
		sumLand = sumLand.Add(r.Allocated.Land)
	}
	if !sumLand.LessThan(pool.Land) {
		t.Errorf("Σunit Land=%s should be < pool.Land=%s", sumLand, pool.Land)
	}
}

// LandPoolSource error harus dipropagasi, bukan diam-diam diabaikan.
func TestService_ComputeAllocation_LandPoolSourceError_Propagates(t *testing.T) {
	sentinel := errors.New("land_stock db down")
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{Land: rupiah(1_000_000)}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	landPool := &mockLandPoolSource{err: sentinel}
	svc := allocation.NewService(store, costProvider, unitSource, allocation.WithLandPoolSource(landPool))

	_, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got %v", err)
	}
}

// Tanpa WithLandPoolSource sama sekali (opsi tidak dipasang) — perilaku identik
// dengan sebelum LT-5 ada, path applyLandPoolOverride short-circuits di awal.
func TestService_ComputeAllocation_NoLandPoolSourceConfigured_Unaffected(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{Land: rupiah(1_000_000)}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	svc := allocation.NewService(store, costProvider, unitSource) // no WithLandPoolSource

	results, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sum domain.Money
	for _, r := range results {
		sum = sum.Add(r.Allocated.Land)
	}
	if !sum.Equal(rupiah(1_000_000)) {
		t.Errorf("Σallocated.Land=%s, want %s", sum, rupiah(1_000_000))
	}
}
