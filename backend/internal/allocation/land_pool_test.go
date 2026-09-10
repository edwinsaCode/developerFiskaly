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
//
// Rule klien UAT #1/#7, DIREVISI Item 9 (UAT 2026-09-07): unit properti kini
// mendapat porsi PROPORSIONAL terhadap land_area (bukan lagi rata); land_stock
// tetap dapat carve-out TETAP = PurchasePricePerM2 × LandAreaM2 (tidak berubah).

// Σ(Allocated) == landPoolCost, persis — Invariant #3.
func TestComputeLandPool_ReconcilesToLastRupiah(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.NewFromInt(150)},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 7, LandAreaM2: decimal.NewFromInt(333), PurchasePricePerM2: rupiah(1_000_000)},
	}
	pool := rupiah(1_000_000_001) // ganjil, memaksa sisa pembulatan

	results, err := allocation.ComputeLandPool(pool, participants)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

// Unit peserta dapat porsi PROPORSIONAL terhadap land_area (Item 9, UAT
// 2026-09-07) — menggantikan rule klien UAT #1 lama (porsi rata, diuji test
// ini sebelum direvisi). area 100:300 (1:3) dari pool 10,000,000 → 2,500,000
// vs 7,500,000, persis (habis dibagi, tanpa sisa pembulatan).
func TestComputeLandPool_UnitsWeightedByArea(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.NewFromInt(300)},
	}
	results, err := allocation.ComputeLandPool(rupiah(10_000_000), participants)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[0].Allocated.Equal(rupiah(2_500_000)) {
		t.Errorf("unit1 (area 100/400)=%s, want 2,500,000", results[0].Allocated)
	}
	if !results[1].Allocated.Equal(rupiah(7_500_000)) {
		t.Errorf("unit2 (area 300/400)=%s, want 7,500,000", results[1].Allocated)
	}
}

// land_area <= 0 pada peserta unit → fail-closed (ErrLandAreaMissing), bukan
// diam-diam menghasilkan HPP Tanah nol untuk unit itu (keputusan produk,
// bukan tebakan — lihat AskUserQuestion 2026-09-07: "Fail-closed per proyek").
func TestComputeLandPool_UnitLandAreaMissing_ReturnsError(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.Zero},
	}
	_, err := allocation.ComputeLandPool(rupiah(10_000_000), participants)
	if !errors.Is(err, allocation.ErrLandAreaMissing) {
		t.Errorf("expected ErrLandAreaMissing, got %v", err)
	}
}

// land_stock TANPA purchase_price (belum diisi admin) → carve-out nol, seluruh
// pool dibagi rata ke unit — tidak diam-diam menyerap sisa pool.
func TestComputeLandPool_LandStockZeroPurchasePrice_GetsZeroCarveOut(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 1, LandAreaM2: decimal.NewFromInt(500)},
	}
	results, err := allocation.ComputeLandPool(rupiah(10_000_000), participants)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !results[1].Allocated.IsZero() {
		t.Errorf("land_stock without purchase_price should get zero carve-out, got %s", results[1].Allocated)
	}
	if !results[0].Allocated.Equal(rupiah(10_000_000)) {
		t.Errorf("sole unit should absorb the full pool, got %s", results[0].Allocated)
	}
}

// land_stock carve-out = PurchasePricePerM2 × LandAreaM2 (JUMLAH TETAP, bukan
// proporsi) — TIDAK berubah oleh Item 9. Sisa pool sekarang dibagi PROPORSIONAL
// terhadap land_area unit (bukan lagi rata): unit1 area 100, unit2 area 300
// (total 400) dari sisa 2,000,000,000 → 500,000,000 vs 1,500,000,000.
func TestComputeLandPool_LandStockFixedCarveOut(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 2, LandAreaM2: decimal.NewFromInt(300)},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 9, LandAreaM2: decimal.NewFromInt(500), PurchasePricePerM2: rupiah(1_000_000)},
	}
	// carve-out = 1,000,000 x 500 = 500,000,000
	results, err := allocation.ComputeLandPool(rupiah(2_500_000_000), participants)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var landStockShare domain.Money
	unitShareByID := map[uint64]domain.Money{}
	for _, r := range results {
		if r.Kind == allocation.LandPoolParticipantLandStock {
			landStockShare = r.Allocated
		} else {
			unitShareByID[r.UnitID] = r.Allocated
		}
	}
	if !landStockShare.Equal(rupiah(500_000_000)) {
		t.Errorf("land_stock carve-out=%s, want 500,000,000", landStockShare)
	}
	// Sisa 2,000,000,000 dibagi proporsional area 100:300 → 500,000,000 vs 1,500,000,000.
	if !unitShareByID[1].Equal(rupiah(500_000_000)) {
		t.Errorf("unit1 (area 100/400)=%s, want 500,000,000", unitShareByID[1])
	}
	if !unitShareByID[2].Equal(rupiah(1_500_000_000)) {
		t.Errorf("unit2 (area 300/400)=%s, want 1,500,000,000", unitShareByID[2])
	}
}

// Carve-out land_stock melebihi total pool → error, bukan porsi unit negatif.
func TestComputeLandPool_LandStockCostExceedsPool_ReturnsError(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantUnit, UnitID: 1, LandAreaM2: decimal.NewFromInt(100)},
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 1, LandAreaM2: decimal.NewFromInt(500), PurchasePricePerM2: rupiah(1_000_000)},
	}
	// carve-out = 500,000,000 > pool 100,000,000
	_, err := allocation.ComputeLandPool(rupiah(100_000_000), participants)
	if !errors.Is(err, allocation.ErrLandStockCostExceedsPool) {
		t.Errorf("expected ErrLandStockCostExceedsPool, got %v", err)
	}
}

// Tidak ada unit penerima sisa pool (hanya land_stock, carve-out < pool) →
// error, bukan sisa yang hilang diam-diam.
func TestComputeLandPool_NoUnitRecipientForRemainder_ReturnsError(t *testing.T) {
	participants := []allocation.LandPoolParticipant{
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 1, LandAreaM2: decimal.NewFromInt(100), PurchasePricePerM2: rupiah(1_000)},
	}
	_, err := allocation.ComputeLandPool(rupiah(1_000_000), participants)
	if !errors.Is(err, allocation.ErrNoLandPoolRecipient) {
		t.Errorf("expected ErrNoLandPoolRecipient, got %v", err)
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

// Proyek DENGAN land_stock: Land unit di-override oleh carve-out tetap +
// sisa-rata, dan Σ(unit Land baru) + land_stock share == pool.Land persis.
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
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 9, LandAreaM2: decimal.NewFromInt(500), PurchasePricePerM2: rupiah(1_000_000)},
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

	// Land unit-only sum harus STRICTLY LESS than pool.Land (land_stock menyerap carve-out tetapnya).
	if !sumLand.LessThan(projectWide.Land) {
		t.Errorf("Σunit Land=%s should be < pool.Land=%s (land_stock must absorb its carve-out)", sumLand, projectWide.Land)
	}

	// Unit-unit harus dapat porsi PROPORSIONAL terhadap land_area (Item 9): area
	// 100 < 150 < 200 → Allocated.Land harus naik strictly mengikuti urutan itu
	// (UnitID 1/2/3 dalam makeUnits() dan landPool.participants urutannya sama).
	byUnitID := map[uint64]domain.Money{}
	for _, r := range results {
		byUnitID[r.UnitID] = r.Allocated.Land
	}
	if !byUnitID[1].LessThan(byUnitID[2]) || !byUnitID[2].LessThan(byUnitID[3]) {
		t.Errorf("Land harus naik seiring land_area (100<150<200): unit1=%s unit2=%s unit3=%s",
			byUnitID[1], byUnitID[2], byUnitID[3])
	}

	// Cross-check dengan ComputeLandPool langsung: unit shares harus identik.
	direct, err := allocation.ComputeLandPool(projectWide.Land, landPool.participants)
	if err != nil {
		t.Fatalf("direct ComputeLandPool: %v", err)
	}
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
		{Kind: allocation.LandPoolParticipantLandStock, LandStockID: 3, LandAreaM2: decimal.NewFromInt(1000), PurchasePricePerM2: rupiah(500_000)},
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
	// Unit-unit harus dapat porsi PROPORSIONAL terhadap land_area (Item 9): area
	// 100 < 150 < 250 → Allocated.Land harus naik strictly mengikuti urutan itu.
	byUnitID := map[uint64]domain.Money{}
	for _, r := range results {
		byUnitID[r.UnitID] = r.Allocated.Land
	}
	if !byUnitID[1].LessThan(byUnitID[2]) || !byUnitID[2].LessThan(byUnitID[3]) {
		t.Errorf("Land harus naik seiring land_area (100<150<250): unit1=%s unit2=%s unit3=%s",
			byUnitID[1], byUnitID[2], byUnitID[3])
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
