package allocation_test

// Tes ComputeHardPool (fungsi murni) — business rule final klien (UAT
// 2026-09-07): Produksi Subsidi dan Produksi Komersial WAJIB terpisah,
// masing-masing hanya boleh membentuk HPP unit dengan TaxCategory yang sama.
// Sarana & Prasarana/Perizinan/legacy digabung ke General, dialokasikan ke
// SEMUA unit sesuai basis alokasi existing.

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
)

// ── Mocks ─────────────────────────────────────────────────────────────────────

type mockHardPoolSource struct {
	pools allocation.HardSubpools
	err   error
}

func (m *mockHardPoolSource) GetHardSubpools(ctx context.Context, tenantID, projectID uint64) (allocation.HardSubpools, error) {
	return m.pools, m.err
}

type mockUnitTaxCategorySource struct {
	byUnit map[uint64]domain.TaxCategory
	err    error
}

func (m *mockUnitTaxCategorySource) GetUnitTaxCategories(ctx context.Context, tenantID, projectID uint64) (map[uint64]domain.TaxCategory, error) {
	return m.byUnit, m.err
}

func weightedResults(weights map[uint64]int64) []allocation.AllocationResult {
	results := make([]allocation.AllocationResult, 0, len(weights))
	for unitID, w := range weights {
		results = append(results, allocation.AllocationResult{
			UnitID: unitID,
			Weight: decimal.NewFromInt(w),
		})
	}
	return results
}

// ── Tests: ComputeHardPool (pure function) ──────────────────────────────────

// Pool General-only (Sarana & Prasarana/Perizinan/legacy) diratakan ke SEMUA
// unit sesuai Weight — perilaku existing sebelum fitur ini, tidak berubah.
func TestComputeHardPool_GeneralOnly_SplitsAcrossAllUnitsByWeight(t *testing.T) {
	results := []allocation.AllocationResult{
		{UnitID: 1, Weight: decimal.NewFromInt(100)},
		{UnitID: 2, Weight: decimal.NewFromInt(300)},
	}
	pools := allocation.HardSubpools{General: rupiah(4_000_000)}

	out, err := allocation.ComputeHardPool(pools, results, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	byUnit := map[uint64]domain.Money{}
	for _, r := range out {
		byUnit[r.UnitID] = r.Allocated.Hard
	}
	if !byUnit[1].Equal(rupiah(1_000_000)) {
		t.Errorf("unit1 (weight 100/400)=%s, want 1,000,000", byUnit[1])
	}
	if !byUnit[2].Equal(rupiah(3_000_000)) {
		t.Errorf("unit2 (weight 300/400)=%s, want 3,000,000", byUnit[2])
	}
}

// Produksi Subsidi HANYA jatuh ke unit dengan TaxCategory=Subsidi — unit
// Komersial harus mendapat Hard=0 dari pool ini.
func TestComputeHardPool_ProduksiSubsidi_OnlyToSubsidiUnits(t *testing.T) {
	results := []allocation.AllocationResult{
		{UnitID: 1, Weight: decimal.NewFromInt(1)}, // subsidi
		{UnitID: 2, Weight: decimal.NewFromInt(1)}, // komersial
	}
	unitTax := map[uint64]domain.TaxCategory{
		1: domain.TaxCategorySubsidi,
		2: domain.TaxCategoryKomersial,
	}
	pools := allocation.HardSubpools{ProduksiSubsidi: rupiah(30_000_000)}

	out, err := allocation.ComputeHardPool(pools, results, unitTax)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	byUnit := map[uint64]domain.Money{}
	for _, r := range out {
		byUnit[r.UnitID] = r.Allocated.Hard
	}
	if !byUnit[1].Equal(rupiah(30_000_000)) {
		t.Errorf("unit subsidi=%s, want 30,000,000 (seluruh pool)", byUnit[1])
	}
	if !byUnit[2].IsZero() {
		t.Errorf("unit komersial=%s, want 0 (tidak boleh tercampur Produksi Subsidi)", byUnit[2])
	}
}

// Produksi Komersial HANYA jatuh ke unit dengan TaxCategory=Komersial —
// simetris dengan test Subsidi di atas.
func TestComputeHardPool_ProduksiKomersial_OnlyToKomersialUnits(t *testing.T) {
	results := []allocation.AllocationResult{
		{UnitID: 1, Weight: decimal.NewFromInt(1)}, // subsidi
		{UnitID: 2, Weight: decimal.NewFromInt(1)}, // komersial
	}
	unitTax := map[uint64]domain.TaxCategory{
		1: domain.TaxCategorySubsidi,
		2: domain.TaxCategoryKomersial,
	}
	pools := allocation.HardSubpools{ProduksiKomersial: rupiah(50_000_000)}

	out, err := allocation.ComputeHardPool(pools, results, unitTax)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	byUnit := map[uint64]domain.Money{}
	for _, r := range out {
		byUnit[r.UnitID] = r.Allocated.Hard
	}
	if !byUnit[2].Equal(rupiah(50_000_000)) {
		t.Errorf("unit komersial=%s, want 50,000,000 (seluruh pool)", byUnit[2])
	}
	if !byUnit[1].IsZero() {
		t.Errorf("unit subsidi=%s, want 0 (tidak boleh tercampur Produksi Komersial)", byUnit[1])
	}
}

// Ketiga pool berjalan simultan tanpa saling mencemari — Scenario D (Full
// HPP): unit subsidi dapat Subsidi+General, unit komersial dapat
// Komersial+General, dan Σ semua == Σ ketiga pool persis (Invariant #3).
func TestComputeHardPool_AllThreePools_NoDoubleCountingNoCrossContamination(t *testing.T) {
	results := []allocation.AllocationResult{
		{UnitID: 1, Weight: decimal.NewFromInt(1)}, // subsidi
		{UnitID: 2, Weight: decimal.NewFromInt(1)}, // komersial
	}
	unitTax := map[uint64]domain.TaxCategory{
		1: domain.TaxCategorySubsidi,
		2: domain.TaxCategoryKomersial,
	}
	pools := allocation.HardSubpools{
		ProduksiSubsidi:   rupiah(30_000_000),
		ProduksiKomersial: rupiah(70_000_000),
		General:           rupiah(10_000_001), // ganjil, memaksa sisa pembulatan
	}

	out, err := allocation.ComputeHardPool(pools, results, unitTax)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sum domain.Money
	byUnit := map[uint64]domain.Money{}
	for _, r := range out {
		sum = sum.Add(r.Allocated.Hard)
		byUnit[r.UnitID] = r.Allocated.Hard
	}
	if !sum.Equal(pools.Total()) {
		t.Errorf("Σallocated.Hard=%s, want %s (== Σ ketiga pool, persis)", sum, pools.Total())
	}
	// unit1 = 30,000,000 (subsidi) + porsi General (50%) = 30,000,000 + 5,000,001 atau 5,000,000
	// unit2 = 70,000,000 (komersial) + porsi General sisanya
	// Cross-contamination check: unit1 tidak boleh dapat porsi ProduksiKomersial,
	// dan sebaliknya — sudah terjamin oleh test lain, di sini fokus reconciliation.
	if byUnit[1].LessThan(rupiah(30_000_000)) || byUnit[2].LessThan(rupiah(70_000_000)) {
		t.Errorf("unit shares turun di bawah pool khusus masing-masing: unit1=%s unit2=%s", byUnit[1], byUnit[2])
	}
}

// Pool Produksi Subsidi > 0 tapi TIDAK ADA unit Subsidi di proyek ini →
// fail-closed, bukan biaya hilang atau jatuh diam-diam ke pool lain.
func TestComputeHardPool_SubsidiPoolNonZero_NoSubsidiUnit_ReturnsError(t *testing.T) {
	results := []allocation.AllocationResult{
		{UnitID: 1, Weight: decimal.NewFromInt(1)}, // komersial saja
	}
	unitTax := map[uint64]domain.TaxCategory{1: domain.TaxCategoryKomersial}
	pools := allocation.HardSubpools{ProduksiSubsidi: rupiah(1_000_000)}

	_, err := allocation.ComputeHardPool(pools, results, unitTax)
	if !errors.Is(err, allocation.ErrHardSubpoolNoSubsidiUnit) {
		t.Errorf("expected ErrHardSubpoolNoSubsidiUnit, got %v", err)
	}
}

// Simetris: pool Produksi Komersial > 0 tanpa unit Komersial → fail-closed.
func TestComputeHardPool_KomersialPoolNonZero_NoKomersialUnit_ReturnsError(t *testing.T) {
	results := []allocation.AllocationResult{
		{UnitID: 1, Weight: decimal.NewFromInt(1)}, // subsidi saja
	}
	unitTax := map[uint64]domain.TaxCategory{1: domain.TaxCategorySubsidi}
	pools := allocation.HardSubpools{ProduksiKomersial: rupiah(1_000_000)}

	_, err := allocation.ComputeHardPool(pools, results, unitTax)
	if !errors.Is(err, allocation.ErrHardSubpoolNoKomersialUnit) {
		t.Errorf("expected ErrHardSubpoolNoKomersialUnit, got %v", err)
	}
}

// unitTaxCategory nil diterima ketika kedua pool klasifikasi nol (General-only
// tidak butuh klasifikasi unit) — dokumentasi eksplisit di ComputeHardPool.
func TestComputeHardPool_GeneralOnly_NilUnitTaxCategory_OK(t *testing.T) {
	results := []allocation.AllocationResult{
		{UnitID: 1, Weight: decimal.NewFromInt(1)},
	}
	pools := allocation.HardSubpools{General: rupiah(1_000_000)}

	_, err := allocation.ComputeHardPool(pools, results, nil)
	if err != nil {
		t.Errorf("General-only tidak boleh butuh unitTaxCategory, got error: %v", err)
	}
}

// Total() harus == Σ ketiga field, dipakai sebagai sanity check reconciliation
// di Service.applyHardPoolOverride sebelum memanggil UnitTaxCategorySource.
func TestHardSubpools_Total_SumsAllThreeFields(t *testing.T) {
	pools := allocation.HardSubpools{
		ProduksiSubsidi:   rupiah(30_000_000),
		ProduksiKomersial: rupiah(70_000_000),
		General:           rupiah(5_000_000),
	}
	if !pools.Total().Equal(rupiah(105_000_000)) {
		t.Errorf("Total()=%s, want 105,000,000", pools.Total())
	}
}

// ── Tests: Service.ComputeAllocation — gating & wiring behavior ─────────────

// Tanpa WithHardPoolSource sama sekali — perilaku identik dengan sebelum
// fitur ini (satu pool Hard diratakan ke semua unit HPP-eligible via basis
// alokasi existing), applyHardPoolOverride short-circuits di awal (NO-OP).
func TestService_ComputeAllocation_NoHardPoolSourceConfigured_Unaffected(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{Hard: rupiah(9_000_000)}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	svc := allocation.NewService(store, costProvider, unitSource) // no WithHardPoolSource

	results, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sum domain.Money
	for _, r := range results {
		sum = sum.Add(r.Allocated.Hard)
	}
	if !sum.Equal(rupiah(9_000_000)) {
		t.Errorf("Σallocated.Hard=%s, want %s", sum, rupiah(9_000_000))
	}
}

// WithHardPoolSource terpasang, pool hanya General (belum ada Subsidi/Komersial
// tercatat) — TIDAK BOLEH memanggil UnitTaxCategorySource sama sekali
// (optimisasi didokumentasikan di applyHardPoolOverride), dan hasil harus
// tetap sama seperti tanpa override.
func TestService_ComputeAllocation_HardPool_GeneralOnly_DoesNotCallUnitTaxSource(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{Hard: rupiah(9_000_000)}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	hardPool := &mockHardPoolSource{pools: allocation.HardSubpools{General: rupiah(9_000_000)}}

	svc := allocation.NewService(store, costProvider, unitSource,
		allocation.WithHardPoolSource(hardPool, nil))

	results, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("unexpected error (should not need UnitTaxCategorySource for General-only): %v", err)
	}
	var sum domain.Money
	for _, r := range results {
		sum = sum.Add(r.Allocated.Hard)
	}
	if !sum.Equal(rupiah(9_000_000)) {
		t.Errorf("Σallocated.Hard=%s, want %s", sum, rupiah(9_000_000))
	}
}

// Ada biaya Produksi Subsidi/Komersial tapi UnitTaxCategorySource TIDAK
// terpasang (nil) → error tegas, bukan panic atau silently skip klasifikasi.
func TestService_ComputeAllocation_HardPool_SubsidiPresent_NoUnitTaxSource_Errors(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{Hard: rupiah(30_000_000)}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()}
	hardPool := &mockHardPoolSource{pools: allocation.HardSubpools{ProduksiSubsidi: rupiah(30_000_000)}}

	svc := allocation.NewService(store, costProvider, unitSource,
		allocation.WithHardPoolSource(hardPool, nil))

	_, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if err == nil {
		t.Fatal("expected error: pool Subsidi/Komersial ada tapi UnitTaxCategorySource tidak terpasang")
	}
}

// End-to-end wiring: HardPoolSource + UnitTaxCategorySource sama-sama
// terpasang, hasil ComputeAllocation() harus cocok dengan ComputeHardPool()
// langsung dipanggil dengan basis Weight yang sama.
func TestService_ComputeAllocation_HardPool_FullWiring_MatchesDirectCompute(t *testing.T) {
	store := &mockConfigStore{config: &allocation.AllocationConfig{
		TenantID: 1, ProjectID: 10, Basis: allocation.BasisSaleableArea,
	}}
	costProvider := &mockDirectCostProvider{breakdown: domain.UnitCostBreakdown{Hard: rupiah(100_000_000)}}
	unitSource := &mockUnitAttributeSource{units: makeUnits()} // UnitID 1,2,3
	hardPool := &mockHardPoolSource{pools: allocation.HardSubpools{
		ProduksiSubsidi:   rupiah(30_000_000),
		ProduksiKomersial: rupiah(40_000_000),
		General:           rupiah(30_000_000),
	}}
	unitTax := &mockUnitTaxCategorySource{byUnit: map[uint64]domain.TaxCategory{
		1: domain.TaxCategorySubsidi,
		2: domain.TaxCategoryKomersial,
		3: domain.TaxCategoryKomersial,
	}}
	svc := allocation.NewService(store, costProvider, unitSource,
		allocation.WithHardPoolSource(hardPool, unitTax))

	results, err := svc.ComputeAllocation(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	direct, err := allocation.ComputeHardPool(hardPool.pools, results, unitTax.byUnit)
	if err != nil {
		t.Fatalf("direct ComputeHardPool: %v", err)
	}
	for i, r := range results {
		if !r.Allocated.Hard.Equal(direct[i].Allocated.Hard) {
			t.Errorf("unit %d: Allocated.Hard=%s, want %s (dari ComputeHardPool langsung)",
				r.UnitID, r.Allocated.Hard, direct[i].Allocated.Hard)
		}
	}
	var sum domain.Money
	for _, r := range results {
		sum = sum.Add(r.Allocated.Hard)
	}
	if !sum.Equal(rupiah(100_000_000)) {
		t.Errorf("Σallocated.Hard=%s, want 100,000,000 (== Σ ketiga pool)", sum)
	}
}
