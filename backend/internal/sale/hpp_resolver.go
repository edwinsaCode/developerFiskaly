package sale

import (
	"context"
	"errors"
	"fmt"

	"github.com/shopspring/decimal"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
)

// HPPResolution adalah hasil penentuan HPP sebuah unit saat BAST: nominal per
// kategori, metode yang dipakai, dan (untuk metode budgeted) draft snapshot yang
// akan dipersist atomik di dalam transaksi BAST.
type HPPResolution struct {
	Breakdown domain.UnitCostBreakdown
	Method    string         // HPPMethodActual | HPPMethodBudgeted
	Snapshot  *SnapshotDraft // nil untuk metode actual/legacy
}

// HPPResolver menentukan HPP unit saat BAST. Dipasang opsional via
// WithHPPResolver; bila tidak dipasang, Service memakai UnitCostProvider legacy
// (metode actual) — menjaga kompatibilitas semua test in-memory yang ada.
type HPPResolver interface {
	ResolveHPP(ctx context.Context, tenantID, projectID uint64, phaseID *uint64, unitID uint64) (HPPResolution, error)
}

// ── Dependency seams (production: *budget.Service, *allocation.Service) ───────

// BudgetBasisSource menyediakan RAB aktif (id+versi+pool kapitalisasi).
// Dipenuhi langsung oleh *budget.Service.GetBudgetedHPPBasis.
type BudgetBasisSource interface {
	GetBudgetedHPPBasis(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (budget.BudgetedHPPBasis, error)
}

// BudgetedUnitAllocator mengalokasikan pool RAB ke unit sesuai basis proyek.
// Dipenuhi langsung oleh *allocation.Service.ComputeBudgeted.
type BudgetedUnitAllocator interface {
	ComputeBudgeted(ctx context.Context, tenantID, projectID uint64, pool domain.UnitCostBreakdown) ([]allocation.AllocationResult, allocation.AllocationBasis, error)
}

// AllocConfigVersionSource menyediakan version basis alokasi aktif (P0-4 D1)
// untuk di-pin ke snapshot. Dipenuhi oleh *allocation.Service.ActiveConfigVersion.
type AllocConfigVersionSource interface {
	ActiveConfigVersion(ctx context.Context, tenantID, projectID uint64) (*allocation.AllocationConfigVersion, error)
}

// BudgetedHPPResolver mengimplementasikan HPPResolver dengan metode Budgeted
// Cost Allocation, dengan fallback ke metode actual (legacy) bila proyek belum
// punya RAB aktif — sehingga proyek/penjualan lama tidak terpengaruh (freeze §4).
type BudgetedHPPResolver struct {
	budget     BudgetBasisSource
	alloc      BudgetedUnitAllocator
	actual     UnitCostProvider         // fallback metode actual (Invariant #4 legacy)
	versionSrc AllocConfigVersionSource // opsional (P0-4 D1): pin version ke snapshot
}

// SetConfigVersionSource memasang sumber version basis (wiring produksi P0-4).
func (r *BudgetedHPPResolver) SetConfigVersionSource(src AllocConfigVersionSource) {
	r.versionSrc = src
}

// NewBudgetedHPPResolver merangkai resolver produksi. actualFallback biasanya
// GORMRepository (yang meng-compute HPP dari jurnal).
func NewBudgetedHPPResolver(b BudgetBasisSource, a BudgetedUnitAllocator, actualFallback UnitCostProvider) *BudgetedHPPResolver {
	return &BudgetedHPPResolver{budget: b, alloc: a, actual: actualFallback}
}

// ResolveHPP memilih metode:
//   - Tidak ada RAB aktif (ErrNoActivePlan) → metode ACTUAL (legacy, backward-compat).
//   - Ada RAB aktif → metode BUDGETED. Basis alokasi WAJIB (gate BCA-2); bila
//     belum diatur → ErrAllocationBasisMissing. HPP unit = porsi teralokasi
//     (Allocated), dan sebuah SnapshotDraft dibuat untuk dipersist saat BAST.
func (r *BudgetedHPPResolver) ResolveHPP(ctx context.Context, tenantID, projectID uint64, phaseID *uint64, unitID uint64) (HPPResolution, error) {
	basis, err := r.budget.GetBudgetedHPPBasis(ctx, tenantID, projectID, phaseID)
	if errors.Is(err, budget.ErrNoActivePlan) {
		// Tanpa RAB aktif → metode actual (proyek legacy). Backward-compat.
		hpp, ferr := r.actual.GetUnitCost(ctx, tenantID, projectID, unitID)
		if ferr != nil {
			return HPPResolution{}, fmt.Errorf("hitung HPP actual: %w", ferr)
		}
		return HPPResolution{Breakdown: hpp, Method: HPPMethodActual}, nil
	}
	if err != nil {
		return HPPResolution{}, fmt.Errorf("ambil basis RAB: %w", err)
	}

	// RAB aktif ada → metode budgeted. Basis alokasi wajib.
	results, allocBasis, err := r.alloc.ComputeBudgeted(ctx, tenantID, projectID, basis.Pool)
	if errors.Is(err, allocation.ErrConfigNotFound) {
		return HPPResolution{}, ErrAllocationBasisMissing
	}
	if err != nil {
		return HPPResolution{}, fmt.Errorf("alokasi HPP budgeted: %w", err)
	}

	var breakdown domain.UnitCostBreakdown
	var unitWeight, unitLandWeight decimal.Decimal
	totalWeight := decimal.Zero
	totalLandWeight := decimal.Zero
	found := false
	for _, res := range results {
		totalWeight = totalWeight.Add(res.Weight)             // denominator basis Hard/Soft/Financing (Σ semua unit)
		totalLandWeight = totalLandWeight.Add(res.LandWeight) // denominator Land = Σ land_area unit (Item 9 — lihat engine.go)
		if res.UnitID == unitID {
			breakdown = res.Allocated // porsi RAB murni; Direct diabaikan (hindari double-count)
			unitWeight = res.Weight
			unitLandWeight = res.LandWeight
			found = true
		}
	}
	if !found {
		return HPPResolution{}, ErrUnitNotFound
	}

	// allocation_percentage = bobot unit / total bobot × 100 (bukti audit).
	pct := decimal.Zero
	if totalWeight.IsPositive() {
		pct = unitWeight.Div(totalWeight).Mul(decimal.NewFromInt(100))
	}
	// Land TIDAK PERNAH ikut basis Hard/Soft/Financing (rule klien UAT #1) —
	// punya basisnya sendiri, land_area per unit (Item 9, UAT 2026-09-07) —
	// audit trail-nya harus mencerminkan itu, bukan basis/pct yang sama dengan
	// kelas lain, supaya "Amount ≈ pool_kelas × pct%" tetap benar untuk Land.
	landPct := decimal.Zero
	if totalLandWeight.IsPositive() {
		landPct = unitLandWeight.Div(totalLandWeight).Mul(decimal.NewFromInt(100))
	}

	draft := &SnapshotDraft{
		ProjectID:                projectID,
		PhaseID:                  phaseID,
		UnitID:                   unitID,
		BudgetPlanID:             basis.PlanID,
		BudgetPlanVersion:        basis.Version,
		Basis:                    string(allocBasis),
		BasisValue:               unitWeight,
		AllocationPercentage:     pct,
		LandBasisValue:           unitLandWeight,
		LandAllocationPercentage: landPct,
		Breakdown:                breakdown,
	}
	// P0-4 D1: pin version basis alokasi (best-effort; kegagalan pin tidak
	// membatalkan BAST — snapshot tanpa pin diperlakukan legacy oleh true-up).
	if r.versionSrc != nil {
		if v, verr := r.versionSrc.ActiveConfigVersion(ctx, tenantID, projectID); verr == nil && v != nil {
			ver := v.Version
			draft.ConfigVersionID = &v.ID
			draft.ConfigVersion = &ver
		}
	}
	return HPPResolution{Breakdown: breakdown, Method: HPPMethodBudgeted, Snapshot: draft}, nil
}
