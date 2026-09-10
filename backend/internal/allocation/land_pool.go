package allocation

import (
	"context"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// LandPoolParticipantKind membedakan peserta pool biaya Land: unit properti
// biasa, atau land_stock (tanah kelebihan proyek yang belum terjual sebagai
// unit properti — LT-3/kelebihan-tanah-final-architecture §E).
type LandPoolParticipantKind string

const (
	LandPoolParticipantUnit      LandPoolParticipantKind = "unit"
	LandPoolParticipantLandStock LandPoolParticipantKind = "land_stock"
)

// LandPoolParticipant adalah satu peserta dalam alokasi pool biaya Land —
// unit properti (units.land_area, dipakai sbg bobot proporsional sejak Item 9,
// UAT 2026-09-07 — sebelumnya hanya identitas peserta karena porsi unit
// selalu RATA) maupun land_stock (land_stock.total_quantity_m2 +
// purchase_price, tanah kelebihan yang akan dijual terpisah via land_sales).
type LandPoolParticipant struct {
	Kind        LandPoolParticipantKind
	UnitID      uint64 // 0 untuk LandPoolParticipantLandStock
	LandStockID uint64 // 0 untuk LandPoolParticipantUnit
	LandAreaM2  decimal.Decimal

	// PurchasePricePerM2 hanya berlaku untuk LandPoolParticipantLandStock —
	// harga beli tanah kelebihan per m² yang sudah diketahui pasti saat
	// setup pool (land_stock.purchase_price). Carve-out land_stock dari pool
	// adalah JUMLAH TETAP = PurchasePricePerM2 × LandAreaM2 (bukan porsi
	// proporsional), konsisten dengan PurchasePriceLandHPPResolver
	// (internal/land/hpp_resolver.go) yang memakai purchase_price yang sama
	// sebagai HPP land_stock itu sendiri saat dijual — Invariant #4 (biaya
	// AKUMULASI, bukan estimasi/alokasi).
	PurchasePricePerM2 domain.Money
}

// LandPoolResult adalah porsi biaya Land yang teralokasi ke satu peserta.
type LandPoolResult struct {
	Kind        LandPoolParticipantKind
	UnitID      uint64
	LandStockID uint64
	Allocated   domain.Money
}

// LandPoolSource menyediakan peserta pool biaya Land berbasis land_area untuk
// sebuah proyek. Opsional pada Service (WithLandPoolSource) — proyek tanpa
// land_stock (mayoritas portofolio) tidak terpengaruh sama sekali; lihat
// applyLandPoolOverride.
type LandPoolSource interface {
	GetLandPoolParticipants(ctx context.Context, tenantID, projectID uint64) ([]LandPoolParticipant, error)
}

// ComputeLandPool mendistribusikan landPoolCost ke setiap peserta menurut
// rule klien UAT #1/#7, direvisi Item 9 (UAT 2026-09-07):
//
//  1. Setiap peserta land_stock mendapat carve-out TETAP = PurchasePricePerM2
//     × LandAreaM2 (harga beli yang sudah pasti — bukan porsi proporsional).
//     Kelebihan Tanah tetap terpisah dari perubahan Item 9 — carve-out ini
//     TIDAK berubah.
//  2. Sisa pool (landPoolCost − Σ carve-out land_stock) dibagi PROPORSIONAL
//     terhadap LandAreaM2 masing-masing unit properti (Item 9 — sebelumnya
//     dibagi RATA; klien mengganti aturan itu agar konsisten dengan basis
//     Land baru di engine.go).
//
// Money.Allocate (largest-remainder) menjamin Σ(Allocated) == landPoolCost,
// persis (Invariant #3). Mengembalikan ErrLandStockCostExceedsPool bila
// carve-out land_stock melebihi pool, ErrNoLandPoolRecipient bila tidak ada
// unit penerima sisa pool (konfigurasi degenerate), dan ErrLandAreaMissing
// (fail-closed, sama seperti buildLandWeights di engine.go) bila ADA unit
// dengan land_area <= 0.
func ComputeLandPool(landPoolCost domain.Money, participants []LandPoolParticipant) ([]LandPoolResult, error) {
	var landStockCarveOut domain.Money
	unitCount := 0
	for _, p := range participants {
		if p.Kind == LandPoolParticipantLandStock {
			area := p.LandAreaM2
			if area.IsNegative() {
				area = decimal.Zero
			}
			landStockCarveOut = landStockCarveOut.Add(p.PurchasePricePerM2.Mul(area))
			continue
		}
		unitCount++
	}

	if landStockCarveOut.GreaterThan(landPoolCost) {
		return nil, ErrLandStockCostExceedsPool
	}
	remainder := landPoolCost.Sub(landStockCarveOut)

	if unitCount == 0 && !remainder.IsZero() {
		return nil, ErrNoLandPoolRecipient
	}

	unitWeights := make([]decimal.Decimal, 0, unitCount)
	for _, p := range participants {
		if p.Kind != LandPoolParticipantUnit {
			continue
		}
		if !p.LandAreaM2.IsPositive() {
			return nil, ErrLandAreaMissing
		}
		unitWeights = append(unitWeights, p.LandAreaM2)
	}
	unitParts := remainder.Allocate(unitWeights)

	results := make([]LandPoolResult, len(participants))
	unitCursor := 0
	for i, p := range participants {
		switch p.Kind {
		case LandPoolParticipantLandStock:
			area := p.LandAreaM2
			if area.IsNegative() {
				area = decimal.Zero
			}
			results[i] = LandPoolResult{
				Kind:        p.Kind,
				UnitID:      p.UnitID,
				LandStockID: p.LandStockID,
				Allocated:   p.PurchasePricePerM2.Mul(area),
			}
		default:
			results[i] = LandPoolResult{
				Kind:        p.Kind,
				UnitID:      p.UnitID,
				LandStockID: p.LandStockID,
				Allocated:   unitParts[unitCursor],
			}
			unitCursor++
		}
	}
	return results, nil
}
