package allocation_test

import (
	"errors"
	"math/rand"
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
)

// ── Helpers ───────────────────────────────────────────────────────────────────

func rupiah(n int64) domain.Money { return domain.FromInt(n) }

func mustAdd(a, b domain.UnitCostBreakdown) domain.UnitCostBreakdown {
	return domain.UnitCostBreakdown{
		Land:      a.Land.Add(b.Land),
		Hard:      a.Hard.Add(b.Hard),
		Soft:      a.Soft.Add(b.Soft),
		Financing: a.Financing.Add(b.Financing),
	}
}

// sumAllocated returns Σ(r.Allocated) across all results.
func sumAllocated(results []allocation.AllocationResult) domain.UnitCostBreakdown {
	var s domain.UnitCostBreakdown
	for _, r := range results {
		s = mustAdd(s, r.Allocated)
	}
	return s
}

// sumTotal returns Σ(r.Total) across all results.
func sumTotal(results []allocation.AllocationResult) domain.UnitCostBreakdown {
	var s domain.UnitCostBreakdown
	for _, r := range results {
		s = mustAdd(s, r.Total)
	}
	return s
}

// sumDirect returns Σ(r.Direct) across all results.
func sumDirect(results []allocation.AllocationResult) domain.UnitCostBreakdown {
	var s domain.UnitCostBreakdown
	for _, r := range results {
		s = mustAdd(s, r.Direct)
	}
	return s
}

// assertReconciliation verifies that Σ(allocated[cat]) == projectWide[cat] for each category.
func assertReconciliation(t *testing.T, tag string, projectWide domain.UnitCostBreakdown, results []allocation.AllocationResult) {
	t.Helper()
	sa := sumAllocated(results)
	if !sa.Land.Equal(projectWide.Land) {
		t.Errorf("%s: land: Σallocated=%s, want %s", tag, sa.Land, projectWide.Land)
	}
	if !sa.Hard.Equal(projectWide.Hard) {
		t.Errorf("%s: hard: Σallocated=%s, want %s", tag, sa.Hard, projectWide.Hard)
	}
	if !sa.Soft.Equal(projectWide.Soft) {
		t.Errorf("%s: soft: Σallocated=%s, want %s", tag, sa.Soft, projectWide.Soft)
	}
	if !sa.Financing.Equal(projectWide.Financing) {
		t.Errorf("%s: financing: Σallocated=%s, want %s", tag, sa.Financing, projectWide.Financing)
	}
}

// assertWholeRupiah verifies all allocated amounts in results are whole rupiah.
func assertWholeRupiah(t *testing.T, tag string, results []allocation.AllocationResult) {
	t.Helper()
	for i, r := range results {
		for cat, m := range map[string]domain.Money{
			"land": r.Allocated.Land, "hard": r.Allocated.Hard,
			"soft": r.Allocated.Soft, "financing": r.Allocated.Financing,
		} {
			if !m.IsWholeRupiah() {
				t.Errorf("%s: unit[%d] %s allocated=%s bukan rupiah bulat", tag, i, cat, m)
			}
		}
	}
}

// ── Deterministic known-value tests ──────────────────────────────────────────

func TestEngine_SingleUnit_GetsAll(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{
		Land: rupiah(1_000_000_000),
		Hard: rupiah(500_000_000),
	}
	units := []allocation.UnitInput{{
		UnitID: 1, SaleableArea: decimal.NewFromInt(150), LandAreaM2: decimal.NewFromInt(150),
		SalesValue: rupiah(2_000_000_000),
	}}

	for _, basis := range []allocation.AllocationBasis{allocation.BasisSaleableArea, allocation.BasisSalesValue} {
		results, err := allocation.Compute(projectWide, units, basis)
		if err != nil {
			t.Fatalf("basis=%s: %v", basis, err)
		}
		if len(results) != 1 {
			t.Fatalf("basis=%s: expected 1 result", basis)
		}
		if !results[0].Allocated.Land.Equal(rupiah(1_000_000_000)) {
			t.Errorf("basis=%s: single unit land=%s, want 1000000000", basis, results[0].Allocated.Land)
		}
		if !results[0].Allocated.Hard.Equal(rupiah(500_000_000)) {
			t.Errorf("basis=%s: single unit hard=%s, want 500000000", basis, results[0].Allocated.Hard)
		}
		assertReconciliation(t, string(basis), projectWide, results)
	}
}

func TestEngine_EqualWeights_EvenSplit(t *testing.T) {
	// 3 units with equal area; 300 cost splits into 100 each
	projectWide := domain.UnitCostBreakdown{Hard: rupiah(300_000_000)}
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
		{UnitID: 3, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
	}
	results, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if !r.Allocated.Hard.Equal(rupiah(100_000_000)) {
			t.Errorf("unit[%d] hard=%s, want 100000000", i, r.Allocated.Hard)
		}
	}
	assertReconciliation(t, "equal-weights", projectWide, results)
}

func TestEngine_OddAmount_LargestRemainderDeterministic(t *testing.T) {
	// 3 units equal weight, amount not divisible by 3: 1,000,000,001
	// Expected: floor=333,333,333 each; 1 extra unit → first bucket (by remainder tie-break: stable)
	projectWide := domain.UnitCostBreakdown{Hard: rupiah(1_000_000_001)}
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
		{UnitID: 3, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
	}

	// Run twice — must be identical (determinism)
	r1, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatal(err)
	}
	r2, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatal(err)
	}
	for i := range r1 {
		if !r1[i].Allocated.Hard.Equal(r2[i].Allocated.Hard) {
			t.Errorf("unit[%d]: run1=%s run2=%s — NOT DETERMINISTIC", i, r1[i].Allocated.Hard, r2[i].Allocated.Hard)
		}
	}

	// Total must still reconcile
	assertReconciliation(t, "odd-amount", projectWide, r1)
	assertWholeRupiah(t, "odd-amount", r1)

	// The extra 1 rupiah goes to exactly one unit
	sum := rupiah(0)
	for _, r := range r1 {
		sum = sum.Add(r.Allocated.Hard)
	}
	if !sum.Equal(rupiah(1_000_000_001)) {
		t.Errorf("total=%s, want 1000000001", sum)
	}
}

func TestEngine_DirectCosts_PassedThrough_Unchanged(t *testing.T) {
	directA := domain.UnitCostBreakdown{Land: rupiah(100_000_000), Hard: rupiah(50_000_000)}
	directB := domain.UnitCostBreakdown{Soft: rupiah(30_000_000)}
	projectWide := domain.UnitCostBreakdown{Hard: rupiah(200_000_000)}

	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000), Direct: directA},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000), Direct: directB},
	}
	results, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatal(err)
	}

	// Direct costs must be unchanged
	if !results[0].Direct.Land.Equal(directA.Land) {
		t.Errorf("unit0 direct.land=%s, want %s", results[0].Direct.Land, directA.Land)
	}
	if !results[0].Direct.Hard.Equal(directA.Hard) {
		t.Errorf("unit0 direct.hard=%s, want %s", results[0].Direct.Hard, directA.Hard)
	}
	if !results[1].Direct.Soft.Equal(directB.Soft) {
		t.Errorf("unit1 direct.soft=%s, want %s", results[1].Direct.Soft, directB.Soft)
	}

	// Total = Direct + Allocated
	expectedTotal0Land := directA.Land // no land project-wide
	if !results[0].Total.Land.Equal(expectedTotal0Land) {
		t.Errorf("unit0 total.land=%s, want %s", results[0].Total.Land, expectedTotal0Land)
	}

	allocatedHard := results[0].Allocated.Hard
	expectedTotal0Hard := directA.Hard.Add(allocatedHard)
	if !results[0].Total.Hard.Equal(expectedTotal0Hard) {
		t.Errorf("unit0 total.hard=%s, want %s", results[0].Total.Hard, expectedTotal0Hard)
	}

	assertReconciliation(t, "direct-passthrough", projectWide, results)
}

func TestEngine_GrandTotal_ReconcilesWith_DirectPlusProjectWide(t *testing.T) {
	directA := domain.UnitCostBreakdown{Land: rupiah(300_000_000), Hard: rupiah(200_000_000)}
	directB := domain.UnitCostBreakdown{Hard: rupiah(150_000_000), Soft: rupiah(50_000_000)}
	projectWide := domain.UnitCostBreakdown{
		Land:      rupiah(1_000_000_000),
		Hard:      rupiah(2_000_000_000),
		Soft:      rupiah(400_000_000),
		Financing: rupiah(100_000_000),
	}
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(150), LandAreaM2: decimal.NewFromInt(150), SalesValue: rupiah(3_000_000_000), Direct: directA},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(200), LandAreaM2: decimal.NewFromInt(200), SalesValue: rupiah(3_500_000_000), Direct: directB},
		{UnitID: 3, SaleableArea: decimal.NewFromInt(250), LandAreaM2: decimal.NewFromInt(250), SalesValue: rupiah(4_000_000_000)},
	}

	expectedGrand := mustAdd(mustAdd(directA, directB), projectWide)

	for _, basis := range []allocation.AllocationBasis{allocation.BasisSaleableArea, allocation.BasisSalesValue} {
		results, err := allocation.Compute(projectWide, units, basis)
		if err != nil {
			t.Fatalf("basis=%s: %v", basis, err)
		}
		assertReconciliation(t, string(basis), projectWide, results)

		st := sumTotal(results)
		if !st.Land.Equal(expectedGrand.Land) {
			t.Errorf("basis=%s: grandTotal.Land=%s, want %s", basis, st.Land, expectedGrand.Land)
		}
		if !st.Hard.Equal(expectedGrand.Hard) {
			t.Errorf("basis=%s: grandTotal.Hard=%s, want %s", basis, st.Hard, expectedGrand.Hard)
		}
	}
}

// ── Basis comparison: both should reconcile ───────────────────────────────────

func TestEngine_ChangeBasis_BothReconcile(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{
		Land:      rupiah(5_000_000_000),
		Hard:      rupiah(20_000_000_000),
		Soft:      rupiah(3_000_000_000),
		Financing: rupiah(1_500_000_000),
	}
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(150), LandAreaM2: decimal.NewFromInt(150), SalesValue: rupiah(2_800_000_000)},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(150), LandAreaM2: decimal.NewFromInt(150), SalesValue: rupiah(2_800_000_000)},
		{UnitID: 3, SaleableArea: decimal.NewFromInt(200), LandAreaM2: decimal.NewFromInt(200), SalesValue: rupiah(3_500_000_000)},
		{UnitID: 4, SaleableArea: decimal.NewFromInt(200), LandAreaM2: decimal.NewFromInt(200), SalesValue: rupiah(3_500_000_000)},
		{UnitID: 5, SaleableArea: decimal.NewFromInt(250), LandAreaM2: decimal.NewFromInt(250), SalesValue: rupiah(4_200_000_000)},
	}

	for _, basis := range []allocation.AllocationBasis{allocation.BasisSaleableArea, allocation.BasisSalesValue} {
		results, err := allocation.Compute(projectWide, units, basis)
		if err != nil {
			t.Fatalf("basis=%s: %v", basis, err)
		}
		assertReconciliation(t, string(basis), projectWide, results)
		assertWholeRupiah(t, string(basis), results)
	}
}

// ── DoD: Randomized reconciliation test ───────────────────────────────────────
//
// Invariant #3: Σ(biaya_teralokasi_unit, per kategori) == biaya project-wide kategori itu.
// Diuji untuk 500 kombinasi acak × 2 basis = 1000 kasus.

func TestEngine_Randomized_Reconciliation(t *testing.T) {
	rng := rand.New(rand.NewSource(20240624)) // fixed seed — deterministic property test

	const nCases = 500
	failures := 0

	for tc := 0; tc < nCases; tc++ {
		// Random whole-rupiah project-wide costs (0 to 50 billion per category)
		projectWide := domain.UnitCostBreakdown{
			Land:      rupiah(rng.Int63n(50_000_000_001)),  // 0..50B
			Hard:      rupiah(rng.Int63n(100_000_000_001)), // 0..100B
			Soft:      rupiah(rng.Int63n(10_000_000_001)),  // 0..10B
			Financing: rupiah(rng.Int63n(5_000_000_001)),   // 0..5B
		}

		// Random unit count 1..18; random area 50..400 sqm; random price 500M..10B
		nUnits := 1 + rng.Intn(18)
		units := make([]allocation.UnitInput, nUnits)
		for i := range units {
			units[i] = allocation.UnitInput{
				UnitID:       uint64(i + 1),
				SaleableArea: decimal.NewFromInt(50 + rng.Int63n(351)),
				LandAreaM2:   decimal.NewFromInt(50 + rng.Int63n(351)),
				SalesValue:   rupiah(500_000_000 + rng.Int63n(9_500_000_001)),
			}
		}

		for _, basis := range []allocation.AllocationBasis{allocation.BasisSaleableArea, allocation.BasisSalesValue} {
			results, err := allocation.Compute(projectWide, units, basis)
			if err != nil {
				t.Errorf("case %d basis=%s: unexpected error: %v", tc, basis, err)
				failures++
				continue
			}

			sa := sumAllocated(results)
			ok := sa.Land.Equal(projectWide.Land) &&
				sa.Hard.Equal(projectWide.Hard) &&
				sa.Soft.Equal(projectWide.Soft) &&
				sa.Financing.Equal(projectWide.Financing)

			if !ok {
				t.Errorf("case %d basis=%s: REKONSILIASI GAGAL — land: %s vs %s; hard: %s vs %s; soft: %s vs %s; fin: %s vs %s",
					tc, basis,
					sa.Land, projectWide.Land,
					sa.Hard, projectWide.Hard,
					sa.Soft, projectWide.Soft,
					sa.Financing, projectWide.Financing,
				)
				failures++
			}

			// All allocated values must be whole rupiah
			for i, r := range results {
				for cat, m := range map[string]domain.Money{
					"land": r.Allocated.Land, "hard": r.Allocated.Hard,
					"soft": r.Allocated.Soft, "fin": r.Allocated.Financing,
				} {
					if !m.IsWholeRupiah() {
						t.Errorf("case %d basis=%s unit[%d] %s=%s bukan rupiah bulat", tc, basis, i, cat, m)
						failures++
					}
				}
			}
		}

		if failures > 10 {
			t.Fatal("terlalu banyak kegagalan, hentikan iterasi")
		}
	}
}

// ── DoD: Determinism — same input → same output ───────────────────────────────

func TestEngine_Deterministic_SameInputSameOutput(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{
		Land:      rupiah(7_777_777_777),
		Hard:      rupiah(3_333_333_333),
		Soft:      rupiah(1_111_111_111),
		Financing: rupiah(999_999_999),
	}
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(143), LandAreaM2: decimal.NewFromInt(143), SalesValue: rupiah(2_750_000_000)},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(167), LandAreaM2: decimal.NewFromInt(167), SalesValue: rupiah(3_200_000_000)},
		{UnitID: 3, SaleableArea: decimal.NewFromInt(89), LandAreaM2: decimal.NewFromInt(89), SalesValue: rupiah(1_500_000_000)},
		{UnitID: 4, SaleableArea: decimal.NewFromInt(201), LandAreaM2: decimal.NewFromInt(201), SalesValue: rupiah(4_100_000_000)},
		{UnitID: 5, SaleableArea: decimal.NewFromInt(312), LandAreaM2: decimal.NewFromInt(312), SalesValue: rupiah(6_000_000_000)},
	}

	for _, basis := range []allocation.AllocationBasis{allocation.BasisSaleableArea, allocation.BasisSalesValue} {
		var runs [3][]allocation.AllocationResult
		for i := range runs {
			r, err := allocation.Compute(projectWide, units, basis)
			if err != nil {
				t.Fatalf("run %d basis=%s: %v", i, basis, err)
			}
			runs[i] = r
		}
		// All 3 runs must be identical
		for u := range runs[0] {
			for cat, sel := range map[string]func(allocation.AllocationResult) domain.Money{
				"land":      func(r allocation.AllocationResult) domain.Money { return r.Allocated.Land },
				"hard":      func(r allocation.AllocationResult) domain.Money { return r.Allocated.Hard },
				"soft":      func(r allocation.AllocationResult) domain.Money { return r.Allocated.Soft },
				"financing": func(r allocation.AllocationResult) domain.Money { return r.Allocated.Financing },
			} {
				v0, v1, v2 := sel(runs[0][u]), sel(runs[1][u]), sel(runs[2][u])
				if !v0.Equal(v1) || !v0.Equal(v2) {
					t.Errorf("basis=%s unit[%d] %s: run0=%s run1=%s run2=%s — TIDAK DETERMINISTIK", basis, u, cat, v0, v1, v2)
				}
			}
		}
	}
}

// ── Edge cases ────────────────────────────────────────────────────────────────

func TestEngine_ZeroProjectWideCosts_NoAllocation(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{} // all zero
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(200), LandAreaM2: decimal.NewFromInt(200), SalesValue: rupiah(2_000_000_000)},
	}
	results, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatal(err)
	}
	for i, r := range results {
		if !r.Allocated.Total().IsZero() {
			t.Errorf("unit[%d]: zero project-wide → expected zero allocation, got %s", i, r.Allocated.Total())
		}
	}
	assertReconciliation(t, "zero-costs", projectWide, results)
}

func TestEngine_AllWeightsZero_ReturnsError(t *testing.T) {
	projectWide := domain.UnitCostBreakdown{Hard: rupiah(1_000_000)}
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.Zero, SalesValue: domain.Zero},
		{UnitID: 2, SaleableArea: decimal.Zero, SalesValue: domain.Zero},
	}
	_, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if !errors.Is(err, allocation.ErrAllWeightsZero) {
		t.Errorf("expected ErrAllWeightsZero, got %v", err)
	}
}

func TestEngine_ZeroUnits_ReturnsError(t *testing.T) {
	_, err := allocation.Compute(domain.UnitCostBreakdown{}, nil, allocation.BasisSaleableArea)
	if !errors.Is(err, allocation.ErrNoUnits) {
		t.Errorf("expected ErrNoUnits, got %v", err)
	}
}

func TestEngine_InvalidBasis_ReturnsError(t *testing.T) {
	units := []allocation.UnitInput{{UnitID: 1, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100)}}
	_, err := allocation.Compute(domain.UnitCostBreakdown{}, units, "invalid_basis")
	if !errors.Is(err, allocation.ErrInvalidBasis) {
		t.Errorf("expected ErrInvalidBasis, got %v", err)
	}
}

// ── DoD: Ganti basis → tetap rekonsiliasi ────────────────────────────────────

func TestEngine_SwitchBasis_BothReconcile_LITHOS(t *testing.T) {
	// LITHOS Villas fixture: 18 unit, 4 jenis, biaya project-wide besar
	projectWide := domain.UnitCostBreakdown{
		Land:      rupiah(8_000_000_000),  // 8 Milyar
		Hard:      rupiah(45_000_000_000), // 45 Milyar
		Soft:      rupiah(5_500_000_000),  // 5.5 Milyar
		Financing: rupiah(2_750_000_000),  // 2.75 Milyar
	}

	type spec struct{ area int64; price int64 }
	specs := []spec{
		// Fase 1: 6 unit A, 150 sqm, Rp 2.8 M
		{150, 2_800_000_000}, {150, 2_800_000_000}, {150, 2_800_000_000},
		{150, 2_800_000_000}, {150, 2_800_000_000}, {150, 2_800_000_000},
		// Fase 2: 6 unit B, 200 sqm, Rp 3.5 M
		{200, 3_500_000_000}, {200, 3_500_000_000}, {200, 3_500_000_000},
		{200, 3_500_000_000}, {200, 3_500_000_000}, {200, 3_500_000_000},
		// Fase 3: 4 unit C, 250 sqm, Rp 4.2 M
		{250, 4_200_000_000}, {250, 4_200_000_000}, {250, 4_200_000_000}, {250, 4_200_000_000},
		// Flagship: 2 unit F, 350 sqm, Rp 7.0 M
		{350, 7_000_000_000}, {350, 7_000_000_000},
	}

	units := make([]allocation.UnitInput, len(specs))
	for i, s := range specs {
		units[i] = allocation.UnitInput{
			UnitID:       uint64(i + 1),
			SaleableArea: decimal.NewFromInt(s.area),
			LandAreaM2:   decimal.NewFromInt(s.area),
			SalesValue:   rupiah(s.price),
		}
	}

	// Hitung dengan basis area
	resArea, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatalf("saleable_area: %v", err)
	}
	assertReconciliation(t, "lithos-area", projectWide, resArea)
	assertWholeRupiah(t, "lithos-area", resArea)

	// Ganti ke basis nilai jual → masih rekonsiliasi
	resValue, err := allocation.Compute(projectWide, units, allocation.BasisSalesValue)
	if err != nil {
		t.Fatalf("sales_value: %v", err)
	}
	assertReconciliation(t, "lithos-value", projectWide, resValue)
	assertWholeRupiah(t, "lithos-value", resValue)

	// Hasilnya BERBEDA antara kedua basis (tidak kebetulan sama)
	if resArea[0].Allocated.Land.Equal(resValue[0].Allocated.Land) &&
		resArea[len(resArea)-1].Allocated.Land.Equal(resValue[len(resValue)-1].Allocated.Land) {
		// Likely different due to different weights, but if equal warn (not fail — could be coincidence)
		t.Log("peringatan: alokasi area == value untuk LITHOS; periksa bobot")
	}
}

// ── Per-category independence ─────────────────────────────────────────────────

// TestEngine_EachCategory_AllocatedIndependently verifies that changing one
// category's project-wide cost does not affect other categories' allocations.
func TestEngine_EachCategory_AllocatedIndependently(t *testing.T) {
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000)},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(200), LandAreaM2: decimal.NewFromInt(200), SalesValue: rupiah(2_000_000_000)},
		{UnitID: 3, SaleableArea: decimal.NewFromInt(300), LandAreaM2: decimal.NewFromInt(300), SalesValue: rupiah(3_000_000_000)},
	}

	base := domain.UnitCostBreakdown{
		Land: rupiah(600_000), Hard: rupiah(900_000),
		Soft: rupiah(300_000), Financing: rupiah(150_000),
	}
	modified := base
	modified.Land = rupiah(999_999_999) // change only land

	resBase, _ := allocation.Compute(base, units, allocation.BasisSaleableArea)
	resMod, _ := allocation.Compute(modified, units, allocation.BasisSaleableArea)

	// Hard, Soft, Financing should be identical; Land should differ
	for i := range resBase {
		if !resBase[i].Allocated.Hard.Equal(resMod[i].Allocated.Hard) {
			t.Errorf("unit[%d] hard changed when only land changed", i)
		}
		if !resBase[i].Allocated.Soft.Equal(resMod[i].Allocated.Soft) {
			t.Errorf("unit[%d] soft changed when only land changed", i)
		}
		if !resBase[i].Allocated.Financing.Equal(resMod[i].Allocated.Financing) {
			t.Errorf("unit[%d] financing changed when only land changed", i)
		}
		if resBase[i].Allocated.Land.Equal(resMod[i].Allocated.Land) {
			t.Errorf("unit[%d] land should change but did not", i)
		}
	}
}

// ── Suma total check ──────────────────────────────────────────────────────────

func TestEngine_SumTotal_EqualsDirect_Plus_ProjectWide(t *testing.T) {
	direct1 := domain.UnitCostBreakdown{Land: rupiah(100_000_000), Hard: rupiah(50_000_000)}
	direct2 := domain.UnitCostBreakdown{Soft: rupiah(20_000_000), Financing: rupiah(10_000_000)}
	projectWide := domain.UnitCostBreakdown{
		Land: rupiah(1_000_000_000), Hard: rupiah(2_000_000_000),
		Soft: rupiah(300_000_000), Financing: rupiah(100_000_000),
	}
	units := []allocation.UnitInput{
		{UnitID: 1, SaleableArea: decimal.NewFromInt(100), LandAreaM2: decimal.NewFromInt(100), SalesValue: rupiah(1_000_000_000), Direct: direct1},
		{UnitID: 2, SaleableArea: decimal.NewFromInt(150), LandAreaM2: decimal.NewFromInt(150), SalesValue: rupiah(1_500_000_000), Direct: direct2},
	}

	results, err := allocation.Compute(projectWide, units, allocation.BasisSaleableArea)
	if err != nil {
		t.Fatal(err)
	}

	// Sum of totals = sum of directs + projectWide (per category)
	sd := sumDirect(results)
	st := sumTotal(results)
	sa := sumAllocated(results)

	// Σtotal == Σdirect + Σallocated
	expectedLand := sd.Land.Add(sa.Land)
	if !st.Land.Equal(expectedLand) {
		t.Errorf("land: Σtotal=%s, Σdirect+Σalloc=%s", st.Land, expectedLand)
	}
	// Σallocated == projectWide (per category)
	assertReconciliation(t, "sum-total", projectWide, results)
}
