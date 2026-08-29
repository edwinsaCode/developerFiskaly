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

// LandPoolParticipant adalah satu peserta dalam alokasi pool biaya Land
// berbasis land_area — baik unit properti (units.land_area) maupun land_stock
// (land_stock.total_quantity_m2, tanah kelebihan yang akan dijual terpisah
// via land_sales).
type LandPoolParticipant struct {
	Kind        LandPoolParticipantKind
	UnitID      uint64 // 0 untuk LandPoolParticipantLandStock
	LandStockID uint64 // 0 untuk LandPoolParticipantUnit
	LandAreaM2  decimal.Decimal
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

// ComputeLandPool mendistribusikan landPoolCost ke setiap peserta secara
// proporsional terhadap land_area, menggunakan Money.Allocate (largest-remainder,
// Invariant #3: Σ(Allocated) == landPoolCost, persis).
//
// Berbeda dari buildWeights di engine.go, di sini TIDAK ada guard bobot-nol
// eksplisit — Money.Allocate sendiri sudah menangani kasus degenerate (semua
// bobot nol → semua nol, tanpa error), yang tepat untuk pool ini: sebagian
// peserta boleh punya land_area nol tanpa itu jadi kondisi galat (mis. unit
// non-tanah yang belum di-backfill).
func ComputeLandPool(landPoolCost domain.Money, participants []LandPoolParticipant) []LandPoolResult {
	weights := make([]decimal.Decimal, len(participants))
	for i, p := range participants {
		w := p.LandAreaM2
		if w.IsNegative() {
			w = decimal.Zero
		}
		weights[i] = w
	}
	parts := landPoolCost.Allocate(weights)

	results := make([]LandPoolResult, len(participants))
	for i, p := range participants {
		results[i] = LandPoolResult{
			Kind:        p.Kind,
			UnitID:      p.UnitID,
			LandStockID: p.LandStockID,
			Allocated:   parts[i],
		}
	}
	return results
}
