package allocation

import (
	"context"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// HardSubpools memecah biaya Konstruksi/Hard Cost project-wide (tier=shared)
// menjadi 3 pool alokasi terpisah, sesuai business rule final klien (UAT
// 2026-09-07): Produksi Subsidi dan Produksi Komersial WAJIB terpisah karena
// nilai biaya produksinya berbeda dan masing-masing hanya boleh membentuk HPP
// unit/blok dengan TaxCategory yang sama. Sarana & Prasarana, Perizinan, dan
// baris shared+hard legacy (belum berklasifikasi/pra-fitur ini) digabung ke
// General — dialokasikan ke SEMUA unit HPP-eligible sesuai basis alokasi
// existing (perilaku sebelum fitur ini, tidak berubah).
type HardSubpools struct {
	ProduksiSubsidi   domain.Money
	ProduksiKomersial domain.Money
	General           domain.Money
}

// Total mengembalikan jumlah ketiga pool — harus sama dengan
// UnitCostBreakdown.Hard project-wide (Invariant #3: tidak ada biaya yang hilang
// atau dobel saat dipecah per subkategori).
func (p HardSubpools) Total() domain.Money {
	return p.ProduksiSubsidi.Add(p.ProduksiKomersial).Add(p.General)
}

// HardPoolSource menyediakan pecahan biaya Konstruksi/Hard Cost project-wide
// per subkategori untuk sebuah proyek. Opsional pada Service
// (WithHardPoolSource) — bila tidak terpasang, ComputeAllocation tetap
// berperilaku seperti sebelum fitur ini (satu pool Hard diratakan ke semua
// unit HPP-eligible).
type HardPoolSource interface {
	GetHardSubpools(ctx context.Context, tenantID, projectID uint64) (HardSubpools, error)
}

// UnitTaxCategorySource menyediakan TaxCategory efektif setiap unit HPP-eligible
// dalam sebuah proyek — project.tax_category sebagai default, product_type
// (unit_type) tax_category sebagai override bila diset. Algoritma identik
// dengan tax.Service (internal/tax/service.go), direplikasi di sini (bukan
// diimpor) karena package allocation tidak mengimpor package project/tax —
// lihat implementasi raw-SQL di GORMRepository.GetUnitTaxCategories.
type UnitTaxCategorySource interface {
	GetUnitTaxCategories(ctx context.Context, tenantID, projectID uint64) (map[uint64]domain.TaxCategory, error)
}

// ComputeHardPool menimpa porsi Hard pada setiap AllocationResult yang
// sebelumnya dihasilkan Compute() (satu pool Hard diratakan ke semua unit),
// menjadi jumlah dari 3 pool terpisah:
//
//   - ProduksiSubsidi   → HANYA unit dengan TaxCategory == Subsidi, proporsional
//     terhadap Weight (basis alokasi proyek yang SUDAH dihitung Compute()).
//   - ProduksiKomersial → HANYA unit dengan TaxCategory == Komersial, proporsional
//     Weight.
//   - General           → SEMUA unit di results, proporsional Weight (perilaku
//     existing sebelum fitur ini).
//
// unitTaxCategory boleh nil/kosong bila pools.ProduksiSubsidi dan
// ProduksiKomersial keduanya nol (General-only tidak butuh klasifikasi unit).
//
// Money.Allocate (largest-remainder) menjamin Σ(bagian per pool) == pool,
// persis (Invariant #3). Mengembalikan ErrHardSubpoolNoSubsidiUnit /
// ErrHardSubpoolNoKomersialUnit (fail-closed) bila pool Subsidi/Komersial > 0
// tapi tidak ada satu pun unit proyek ini yang cocok TaxCategory-nya — biaya
// tidak boleh diam-diam jatuh ke pool lain atau hilang.
func ComputeHardPool(pools HardSubpools, results []AllocationResult, unitTaxCategory map[uint64]domain.TaxCategory) ([]AllocationResult, error) {
	subsidiWeights := make([]decimal.Decimal, 0, len(results))
	subsidiIdx := make([]int, 0, len(results))
	komersialWeights := make([]decimal.Decimal, 0, len(results))
	komersialIdx := make([]int, 0, len(results))
	generalWeights := make([]decimal.Decimal, len(results))

	for i, r := range results {
		generalWeights[i] = r.Weight
		switch unitTaxCategory[r.UnitID] {
		case domain.TaxCategorySubsidi:
			subsidiWeights = append(subsidiWeights, r.Weight)
			subsidiIdx = append(subsidiIdx, i)
		case domain.TaxCategoryKomersial:
			komersialWeights = append(komersialWeights, r.Weight)
			komersialIdx = append(komersialIdx, i)
		}
	}

	if !pools.ProduksiSubsidi.IsZero() && len(subsidiIdx) == 0 {
		return nil, ErrHardSubpoolNoSubsidiUnit
	}
	if !pools.ProduksiKomersial.IsZero() && len(komersialIdx) == 0 {
		return nil, ErrHardSubpoolNoKomersialUnit
	}

	subsidiParts := pools.ProduksiSubsidi.Allocate(subsidiWeights)
	komersialParts := pools.ProduksiKomersial.Allocate(komersialWeights)
	generalParts := pools.General.Allocate(generalWeights)

	hardByUnit := make([]domain.Money, len(results))
	copy(hardByUnit, generalParts)
	for k, i := range subsidiIdx {
		hardByUnit[i] = hardByUnit[i].Add(subsidiParts[k])
	}
	for k, i := range komersialIdx {
		hardByUnit[i] = hardByUnit[i].Add(komersialParts[k])
	}

	out := make([]AllocationResult, len(results))
	for i, r := range results {
		r.Allocated.Hard = hardByUnit[i]
		r.Total.Hard = r.Direct.Hard.Add(hardByUnit[i])
		out[i] = r
	}
	return out, nil
}
