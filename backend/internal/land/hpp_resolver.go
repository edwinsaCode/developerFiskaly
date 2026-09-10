package land

// LT-5 (kelebihan-tanah-final-architecture §E.4) — "ResolveLandHPPRate:
// resolver yang sama, diperluas". Rantai finalized→budgeted→actual yang sudah
// dipakai internal/sale.hpp_resolver.go untuk unit properti dipakai PERSIS
// SAMA di sini — bedanya hanya angka yang di-resolve adalah tarif per-m²
// (hpp_rate_per_m2), bukan lump-sum per unit. Bukan resolver kedua.
//
// KOREKSI KLIEN (2026-08-31): BudgetedLandHPPResolver di bawah TIDAK LAGI
// dipasang di produksi. Kelebihan Tanah adalah parsel yang dibeli terpisah
// dengan harga per-m² yang sudah diketahui pasti (land_stock.purchase_price)
// — memakainya LANGSUNG sebagai tarif HPP lebih sesuai Invariant #4 (biaya
// AKUMULASI, bukan estimasi/alokasi) daripada membaginya dari pool RAB/actual
// project-wide, yang cocok untuk tanah di bawah unit rumah (tak punya harga
// beli sendiri) tapi tidak untuk tanah lebih yang harga belinya sudah pasti.
// Lihat PurchasePriceLandHPPResolver di bawah — resolver produksi saat ini.

import (
	"context"
	"errors"
	"fmt"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
)

const (
	HPPMethodActual        = "actual"
	HPPMethodBudgeted      = "budgeted"
	HPPMethodPurchasePrice = "purchase_price"
)

// ErrLandStockHasNoQuantity is returned when resolving a rate for a
// land_stock whose total_quantity_m2 is zero — division by zero guard.
var ErrLandStockHasNoQuantity = errors.New("land_stock belum punya total_quantity_m2, tarif HPP per m2 tidak dapat dihitung")

// LandHPPResolution is the outcome of resolving a per-m² HPP rate for a
// project's land_stock at Akad time — mirrors sale.HPPResolution.
type LandHPPResolution struct {
	RatePerM2 domain.Money
	Method    string // HPPMethodActual | HPPMethodBudgeted

	// Snapshot metadata — populated only for HPPMethodBudgeted; nil fields for
	// HPPMethodActual (implicit method encoding on land_allocations, no
	// separate hpp_method column — pola identik allocation_snapshots).
	BudgetPlanID      *uint64
	BudgetPlanVersion *int
	ConfigVersionID   *uint64
	ConfigVersion     *int
	Basis             string
}

// LandHPPResolver menentukan tarif HPP per m² land_stock proyek saat Akad.
type LandHPPResolver interface {
	ResolveLandHPPRate(ctx context.Context, tenantID, projectID uint64) (LandHPPResolution, error)
}

// ── Dependency seams (production: *budget.Service, *allocation.Service) ────

// LandBudgetBasisSource menyediakan RAB aktif (pool kapitalisasi proyek).
// Dipenuhi langsung oleh *budget.Service.GetBudgetedHPPBasis. phaseID selalu
// nil — land_stock adalah pool project-scoped, bukan per-fase (§B.1).
type LandBudgetBasisSource interface {
	GetBudgetedHPPBasis(ctx context.Context, tenantID, projectID uint64, phaseID *uint64) (budget.BudgetedHPPBasis, error)
}

// LandStockShareComputer mengembalikan porsi pool biaya Land yang jadi hak
// land_stock proyek — dipenuhi *allocation.Service.
type LandStockShareComputer interface {
	ComputeLandStockShare(ctx context.Context, tenantID, projectID uint64, landPoolCost domain.Money) (domain.Money, bool, error)
	ComputeLandStockShareActual(ctx context.Context, tenantID, projectID uint64) (domain.Money, bool, error)
}

// LandAllocConfigVersionSource menyediakan version basis alokasi aktif
// (pola identik sale.AllocConfigVersionSource) — opsional, best-effort.
type LandAllocConfigVersionSource interface {
	ActiveConfigVersion(ctx context.Context, tenantID, projectID uint64) (*allocation.AllocationConfigVersion, error)
}

// BudgetedLandHPPResolver implements LandHPPResolver: metode Budgeted Cost
// Allocation, fallback ke metode actual bila proyek belum punya RAB aktif —
// pola identik sale.BudgetedHPPResolver.
type BudgetedLandHPPResolver struct {
	budget     LandBudgetBasisSource
	share      LandStockShareComputer
	stock      LandStockQuantitySource
	versionSrc LandAllocConfigVersionSource // opsional: pin version ke snapshot
}

// LandStockQuantitySource menyediakan total_quantity_m2 land_stock proyek —
// denominator tarif per m². Dipenuhi oleh Store.FindPoolByProject.
type LandStockQuantitySource interface {
	FindPoolByProject(ctx context.Context, tenantID, projectID uint64) (*LandStock, error)
}

// NewBudgetedLandHPPResolver merangkai resolver produksi.
func NewBudgetedLandHPPResolver(b LandBudgetBasisSource, s LandStockShareComputer, stock LandStockQuantitySource) *BudgetedLandHPPResolver {
	return &BudgetedLandHPPResolver{budget: b, share: s, stock: stock}
}

// SetConfigVersionSource memasang sumber version basis (wiring produksi).
func (r *BudgetedLandHPPResolver) SetConfigVersionSource(src LandAllocConfigVersionSource) {
	r.versionSrc = src
}

// ResolveLandHPPRate memilih metode:
//   - Tidak ada RAB aktif (ErrNoActivePlan) → metode ACTUAL: porsi land_stock
//     dari pool biaya AKTUAL project-wide (jurnal), dibagi total_quantity_m2.
//   - Ada RAB aktif → metode BUDGETED: porsi land_stock dari pool RAB, dibagi
//     total_quantity_m2.
//
// Kedua metode memakai land_stock.total_quantity_m2 SELURUH stok (bukan hanya
// sisa AVAILABLE) sebagai denominator (§E.1: "supaya persentase tiap peserta
// stabil tidak tergantung urutan penjualan") — pola identik cara unit properti
// membagi pool meski belum semua unit terjual.
func (r *BudgetedLandHPPResolver) ResolveLandHPPRate(ctx context.Context, tenantID, projectID uint64) (LandHPPResolution, error) {
	stock, err := r.stock.FindPoolByProject(ctx, tenantID, projectID)
	if err != nil {
		return LandHPPResolution{}, fmt.Errorf("ambil land_stock: %w", err)
	}
	if !stock.TotalQuantityM2.IsPositive() {
		return LandHPPResolution{}, ErrLandStockHasNoQuantity
	}

	basis, err := r.budget.GetBudgetedHPPBasis(ctx, tenantID, projectID, nil)
	if errors.Is(err, budget.ErrNoActivePlan) {
		share, ok, serr := r.share.ComputeLandStockShareActual(ctx, tenantID, projectID)
		if serr != nil {
			return LandHPPResolution{}, fmt.Errorf("hitung porsi land_stock actual: %w", serr)
		}
		if !ok {
			return LandHPPResolution{}, ErrLandStockNotFound
		}
		rate := domain.FromDecimal(share.Decimal().DivRound(stock.TotalQuantityM2, 4))
		return LandHPPResolution{RatePerM2: rate, Method: HPPMethodActual}, nil
	}
	if err != nil {
		return LandHPPResolution{}, fmt.Errorf("ambil basis RAB: %w", err)
	}

	share, ok, serr := r.share.ComputeLandStockShare(ctx, tenantID, projectID, basis.Pool.Land)
	if serr != nil {
		return LandHPPResolution{}, fmt.Errorf("hitung porsi land_stock budgeted: %w", serr)
	}
	if !ok {
		return LandHPPResolution{}, ErrLandStockNotFound
	}
	rate := domain.FromDecimal(share.Decimal().DivRound(stock.TotalQuantityM2, 4))

	res := LandHPPResolution{
		RatePerM2:         rate,
		Method:            HPPMethodBudgeted,
		BudgetPlanID:      &basis.PlanID,
		BudgetPlanVersion: &basis.Version,
		Basis:             "land_area",
	}
	// Pin version basis alokasi (best-effort; kegagalan pin tidak membatalkan
	// Akad — pola identik sale.BudgetedHPPResolver).
	if r.versionSrc != nil {
		if v, verr := r.versionSrc.ActiveConfigVersion(ctx, tenantID, projectID); verr == nil && v != nil {
			ver := v.Version
			res.ConfigVersionID = &v.ID
			res.ConfigVersion = &ver
		}
	}
	return res, nil
}

// PurchasePriceLandHPPResolver implements LandHPPResolver: HPP per m² =
// land_stock.purchase_price, langsung, tanpa alokasi RAB/actual — resolver
// PRODUKSI saat ini (koreksi klien 2026-08-31, lihat komentar header file).
//
// Kelebihan Tanah dibeli sebagai parsel terpisah dengan harga per-m² yang
// sudah pasti diketahui saat setup pool — memakainya langsung sebagai HPP
// adalah biaya AKUMULASI sungguhan (Invariant #4), bukan estimasi/alokasi.
type PurchasePriceLandHPPResolver struct {
	stock LandStockQuantitySource
}

// NewPurchasePriceLandHPPResolver merangkai resolver produksi.
func NewPurchasePriceLandHPPResolver(stock LandStockQuantitySource) *PurchasePriceLandHPPResolver {
	return &PurchasePriceLandHPPResolver{stock: stock}
}

func (r *PurchasePriceLandHPPResolver) ResolveLandHPPRate(ctx context.Context, tenantID, projectID uint64) (LandHPPResolution, error) {
	stock, err := r.stock.FindPoolByProject(ctx, tenantID, projectID)
	if err != nil {
		return LandHPPResolution{}, fmt.Errorf("ambil land_stock: %w", err)
	}
	if !stock.TotalQuantityM2.IsPositive() {
		return LandHPPResolution{}, ErrLandStockHasNoQuantity
	}
	return LandHPPResolution{
		RatePerM2: stock.PurchasePrice,
		Method:    HPPMethodPurchasePrice,
		Basis:     "purchase_price",
	}, nil
}
