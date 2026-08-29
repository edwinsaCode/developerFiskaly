package allocation

import (
	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// Compute mendistribusikan projectWideCosts ke setiap unit secara proporsional
// berdasarkan basis yang dipilih, menggunakan metode largest-remainder sehingga
// Σ(allocated per unit, per kategori) == projectWideCosts untuk kategori itu, persis.
//
// Invariant #3 terjamin: Money.Allocate sudah menjamin Σ(parts) == original.
// Invariant #4 ditegakkan oleh caller (Phase 6) yang menggunakan Total = Direct + Allocated.
//
// Phase 5 hanya MENGHITUNG — tidak ada posting jurnal di sini.
func Compute(
	projectWideCosts domain.UnitCostBreakdown,
	units []UnitInput,
	basis AllocationBasis,
) ([]AllocationResult, error) {
	if len(units) == 0 {
		return nil, ErrNoUnits
	}
	if !basis.Valid() {
		return nil, ErrInvalidBasis
	}

	weights, err := buildWeights(units, basis)
	if err != nil {
		return nil, err
	}

	// Alokasikan setiap kategori secara independen menggunakan Money.Allocate.
	// Jika projectWideCosts.Land == 0, hasilnya semua nol — tetap rekonsiliasi.
	landParts := projectWideCosts.Land.Allocate(weights)
	hardParts := projectWideCosts.Hard.Allocate(weights)
	softParts := projectWideCosts.Soft.Allocate(weights)
	finParts := projectWideCosts.Financing.Allocate(weights)

	results := make([]AllocationResult, len(units))
	for i, u := range units {
		allocated := domain.UnitCostBreakdown{
			Land:      landParts[i],
			Hard:      hardParts[i],
			Soft:      softParts[i],
			Financing: finParts[i],
		}
		total := domain.UnitCostBreakdown{
			Land:      u.Direct.Land.Add(landParts[i]),
			Hard:      u.Direct.Hard.Add(hardParts[i]),
			Soft:      u.Direct.Soft.Add(softParts[i]),
			Financing: u.Direct.Financing.Add(finParts[i]),
		}
		results[i] = AllocationResult{
			UnitID:    u.UnitID,
			Direct:    u.Direct,
			Allocated: allocated,
			Total:     total,
			Weight:    weights[i],
		}
	}

	return results, nil
}

// buildWeights mengekstrak bobot dari setiap unit sesuai basis.
// Mengembalikan ErrAllWeightsZero jika semua bobot nol — untuk mencegah
// Money.Allocate mengembalikan semua-nol yang melanggar Invariant #3.
func buildWeights(units []UnitInput, basis AllocationBasis) ([]decimal.Decimal, error) {
	weights := make([]decimal.Decimal, len(units))
	totalWeight := decimal.Zero

	for i, u := range units {
		var w decimal.Decimal
		switch basis {
		case BasisSaleableArea:
			w = u.SaleableArea
		case BasisSalesValue:
			w = u.SalesValue.Decimal()
		}
		if w.IsNegative() {
			w = decimal.Zero
		}
		weights[i] = w
		totalWeight = totalWeight.Add(w)
	}

	if totalWeight.IsZero() {
		return nil, ErrAllWeightsZero
	}

	return weights, nil
}
