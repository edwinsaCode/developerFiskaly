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

	// RULE KLIEN (UAT, direvisi Item 9 — UAT 2026-09-07): HPP Tanah TIDAK
	// PERNAH mengikuti basis alokasi Hard/Soft/Financing (saleable_area/
	// sales_value) — Land selalu punya basisnya SENDIRI, terlepas dari basis
	// yang dipilih proyek. Sebelumnya basis Land itu "rata" (equalWeights);
	// klien mengganti aturan itu: porsi HPP Tanah per unit sekarang
	// proporsional terhadap land_area unit (luas tanah × harga tanah per m²,
	// tersirat dari pool ÷ Σ land_area — Money.Allocate sudah proporsional).
	// Kelebihan Tanah (land_stock, LT-5) tetap terpisah — lihat land_pool.go.
	landWeights, err := buildLandWeights(units)
	if err != nil {
		return nil, err
	}

	// Alokasikan setiap kategori secara independen menggunakan Money.Allocate.
	// Jika projectWideCosts.Land == 0, hasilnya semua nol — tetap rekonsiliasi.
	landParts := projectWideCosts.Land.Allocate(landWeights)
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
		// Weight adalah bukti audit basis Hard/Soft/Financing (saleable_area
		// atau sales_value) — TIDAK mencerminkan Land, yang punya basisnya
		// sendiri: land_area per unit (landWeights, di atas — Item 9). LandWeight
		// membawa bukti audit Land secara terpisah agar snapshot tidak salah
		// mengklaim Land memakai basis yang sama dengan Hard/Soft/Financing.
		results[i] = AllocationResult{
			UnitID:     u.UnitID,
			Direct:     u.Direct,
			Allocated:  allocated,
			Total:      total,
			Weight:     weights[i],
			LandWeight: landWeights[i],
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

// buildLandWeights (Item 9, UAT 2026-09-07): bobot Land per unit = LandAreaM2
// unit itu — HPP Tanah per unit sekarang proporsional terhadap luas tanah,
// bukan lagi dibagi rata (rule klien UAT #1 lama).
//
// Fail-closed per keputusan produk (bukan tebakan): menolak jika ADA unit
// dengan land_area <= 0 — bukan hanya bila TOTALnya nol — supaya HPP Tanah
// unit itu tidak diam-diam menjadi 0 hanya karena datanya belum sempat diisi
// admin (units.land_area DEFAULT 0, opsional saat create unit — lihat
// internal/project/service.go). Lihat ErrLandAreaMissing.
func buildLandWeights(units []UnitInput) ([]decimal.Decimal, error) {
	weights := make([]decimal.Decimal, len(units))
	for i, u := range units {
		if !u.LandAreaM2.IsPositive() {
			return nil, ErrLandAreaMissing
		}
		weights[i] = u.LandAreaM2
	}
	return weights, nil
}
