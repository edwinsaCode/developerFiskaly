package sale_test

import (
	"context"
	"errors"
	"testing"

	"github.com/shopspring/decimal"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
	"esaproperti/internal/sale"
)

// P0-2/P0-3 — unit test resolver HPP (budgeted vs actual, gate, snapshot draft).

type fakeBudgetBasis struct {
	basis budget.BudgetedHPPBasis
	err   error
}

func (f fakeBudgetBasis) GetBudgetedHPPBasis(_ context.Context, _, _ uint64, _ *uint64) (budget.BudgetedHPPBasis, error) {
	return f.basis, f.err
}

type fakeAllocator struct {
	results []allocation.AllocationResult
	basis   allocation.AllocationBasis
	err     error
}

func (f fakeAllocator) ComputeBudgeted(_ context.Context, _, _ uint64, _ domain.UnitCostBreakdown) ([]allocation.AllocationResult, allocation.AllocationBasis, error) {
	return f.results, f.basis, f.err
}

type fakeActual struct {
	bd  domain.UnitCostBreakdown
	err error
}

func (f fakeActual) GetUnitCost(_ context.Context, _, _, _ uint64) (domain.UnitCostBreakdown, error) {
	return f.bd, f.err
}

func money(v int64) domain.Money { return domain.FromInt(v) }

// RAB aktif + basis + unit ada → metode budgeted, breakdown = Allocated, snapshot
// membekukan versi RAB (plan id + version) dan basis.
func TestResolveHPP_Budgeted(t *testing.T) {
	unitShare := domain.UnitCostBreakdown{Land: money(100), Hard: money(200), Financing: money(50)}
	r := sale.NewBudgetedHPPResolver(
		fakeBudgetBasis{basis: budget.BudgetedHPPBasis{
			PlanID: 7, Version: 3,
			Pool: domain.UnitCostBreakdown{Land: money(1000), Hard: money(2000), Financing: money(500)},
		}},
		fakeAllocator{
			basis: allocation.BasisSaleableArea,
			results: []allocation.AllocationResult{
				{UnitID: 99, Allocated: domain.UnitCostBreakdown{Land: money(900)}, Weight: decimal.NewFromInt(300)}, // unit lain
				{UnitID: 42, Allocated: unitShare, Weight: decimal.NewFromInt(100)},                                  // 100/400 = 25%
			},
		},
		fakeActual{}, // tidak dipakai di jalur budgeted
	)

	res, err := r.ResolveHPP(context.Background(), 1, 10, nil, 42)
	if err != nil {
		t.Fatalf("ResolveHPP: %v", err)
	}
	if res.Method != sale.HPPMethodBudgeted {
		t.Errorf("method: got %q, want budgeted", res.Method)
	}
	if res.Breakdown.Total().String() != "350" {
		t.Errorf("breakdown total: got %s, want 350", res.Breakdown.Total())
	}
	if res.Snapshot == nil {
		t.Fatal("snapshot draft harus terisi di metode budgeted")
	}
	if res.Snapshot.BudgetPlanID != 7 || res.Snapshot.BudgetPlanVersion != 3 {
		t.Errorf("snapshot RAB ref: got plan=%d ver=%d, want 7/3", res.Snapshot.BudgetPlanID, res.Snapshot.BudgetPlanVersion)
	}
	if res.Snapshot.Basis != string(allocation.BasisSaleableArea) {
		t.Errorf("snapshot basis: got %q, want saleable_area", res.Snapshot.Basis)
	}
	if res.Snapshot.UnitID != 42 {
		t.Errorf("snapshot unit: got %d, want 42", res.Snapshot.UnitID)
	}
	// Bukti basis: unit 42 weight 100 dari total 400 → 25%.
	if res.Snapshot.BasisValue.String() != "100" {
		t.Errorf("snapshot basis_value: got %s, want 100", res.Snapshot.BasisValue)
	}
	if !res.Snapshot.AllocationPercentage.Equal(decimal.NewFromInt(25)) {
		t.Errorf("snapshot allocation_percentage: got %s, want 25", res.Snapshot.AllocationPercentage)
	}
}

// Tidak ada RAB aktif → metode actual (legacy/backward-compat), tanpa snapshot.
func TestResolveHPP_ActualFallback(t *testing.T) {
	actual := domain.UnitCostBreakdown{Land: money(111), Hard: money(222)}
	r := sale.NewBudgetedHPPResolver(
		fakeBudgetBasis{err: budget.ErrNoActivePlan},
		fakeAllocator{err: errors.New("tidak boleh dipanggil")},
		fakeActual{bd: actual},
	)

	res, err := r.ResolveHPP(context.Background(), 1, 10, nil, 42)
	if err != nil {
		t.Fatalf("ResolveHPP: %v", err)
	}
	if res.Method != sale.HPPMethodActual {
		t.Errorf("method: got %q, want actual", res.Method)
	}
	if res.Snapshot != nil {
		t.Error("metode actual tidak boleh menghasilkan snapshot")
	}
	if res.Breakdown.Total().String() != "333" {
		t.Errorf("breakdown total: got %s, want 333", res.Breakdown.Total())
	}
}

// RAB aktif tapi basis alokasi belum diatur → ErrAllocationBasisMissing (gate BCA-2).
func TestResolveHPP_BasisMissing(t *testing.T) {
	r := sale.NewBudgetedHPPResolver(
		fakeBudgetBasis{basis: budget.BudgetedHPPBasis{PlanID: 1, Version: 1}},
		fakeAllocator{err: allocation.ErrConfigNotFound},
		fakeActual{},
	)

	_, err := r.ResolveHPP(context.Background(), 1, 10, nil, 42)
	if !errors.Is(err, sale.ErrAllocationBasisMissing) {
		t.Fatalf("want ErrAllocationBasisMissing, got %v", err)
	}
}

// Unit tidak ada dalam hasil alokasi → ErrUnitNotFound.
func TestResolveHPP_UnitNotInResults(t *testing.T) {
	r := sale.NewBudgetedHPPResolver(
		fakeBudgetBasis{basis: budget.BudgetedHPPBasis{PlanID: 1, Version: 1}},
		fakeAllocator{basis: allocation.BasisSalesValue, results: []allocation.AllocationResult{{UnitID: 1}}},
		fakeActual{},
	)

	_, err := r.ResolveHPP(context.Background(), 1, 10, nil, 42)
	if !errors.Is(err, sale.ErrUnitNotFound) {
		t.Fatalf("want ErrUnitNotFound, got %v", err)
	}
}
