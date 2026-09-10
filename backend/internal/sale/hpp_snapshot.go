package sale

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// Metode penurunan HPP yang dicatat di sale_records.hpp_method.
const (
	// HPPMethodActual: HPP = biaya akumulasi aktual per unit dari jurnal
	// (metode legacy; juga jalur backward-compat untuk proyek tanpa RAB aktif).
	HPPMethodActual = "actual"
	// HPPMethodBudgeted: HPP = porsi RAB teranggarkan yang dialokasikan ke unit
	// (Budgeted Cost Allocation — docs/budgeted-cost-allocation-spec.md).
	HPPMethodBudgeted = "budgeted"
	// HPPMethodFinalized (P0-4 D2): unit dijual SETELAH project completion
	// finalized + true-up posted → HPP = act_HPP finalized dari hpp_trueup_lines
	// (tanpa alokasi live baru, tanpa snapshot baru).
	HPPMethodFinalized = "finalized"
	// HPPMethodNone (Product Catalog H-1): produk non-properti — TIDAK ikut
	// alokasi HPP sama sekali. Pendapatan tetap diakui, tetapi tidak ada Event 4
	// (COGS) dan tidak ada snapshot alokasi. Bukan "HPP nol karena belum ada
	// biaya", melainkan "produk ini memang tidak punya HPP proyek".
	HPPMethodNone = "none"
)

// AllocationSnapshot membekukan input+output alokasi HPP budgeted pada saat BAST.
// APPEND-ONLY (Invariant #5): tidak pernah di-UPDATE/DELETE. Membuat HPP
// budgeted auditable & reproducible walau RAB kemudian di-supersede — karena
// BudgetPlanID + BudgetPlanVersion membekukan versi RAB yang dipakai.
type AllocationSnapshot struct {
	ID                uint64  `gorm:"primaryKey;autoIncrement"                     json:"id"`
	TenantID          uint64  `gorm:"not null;index"                               json:"-"`
	ProjectID         uint64  `gorm:"not null;index"                               json:"project_id"`
	PhaseID           *uint64 `gorm:"index"                                        json:"phase_id,omitempty"`
	UnitID            uint64  `gorm:"not null;uniqueIndex"                         json:"unit_id"`
	BudgetPlanID      uint64  `gorm:"not null;index"                               json:"budget_plan_id"`
	BudgetPlanVersion int     `gorm:"not null;default:1"                           json:"budget_plan_version"`
	Basis             string  `gorm:"not null;size:20"                             json:"basis"`
	// P0-4 D1 (migration 000030): pin version basis alokasi yang dipakai saat
	// BAST. NULL = snapshot pra-P0-4 (diperlakukan sebagai basis aktif saat itu).
	AllocationConfigVersionID *uint64      `gorm:"column:allocation_config_version_id"          json:"allocation_config_version_id,omitempty"`
	AllocationConfigVersion   *int         `gorm:"column:allocation_config_version"             json:"allocation_config_version,omitempty"`
	HPPTotal                  domain.Money `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"hpp_total"`
	CreatedAt                 time.Time    `                                                    json:"created_at"`
	UpdatedAt                 time.Time    `                                                    json:"updated_at"`

	Lines []AllocationSnapshotLine `gorm:"-" json:"lines,omitempty"`
}

func (AllocationSnapshot) TableName() string { return "allocation_snapshots" }

// AllocationSnapshotLine adalah rincian HPP budgeted per accounting_class
// (land/hard/soft/financing). InventoryAccountCode di-denormalisasi dari
// taxonomy (domain.CostCategory.InventoryAccountCode) untuk audit yang berdiri
// sendiri. Σ(amount seluruh unit di plan yang sama, per class) == pool RAB per
// class (BCA-1).
type AllocationSnapshotLine struct {
	ID         uint64 `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID   uint64 `gorm:"not null;index"           json:"-"`
	SnapshotID uint64 `gorm:"not null;index"           json:"snapshot_id"`
	// Identitas historis (P0-4 Req#1, migration 000030): snapshot terbaca auditor
	// bertahun kemudian TANPA join master. unit_id 0 / nama '' = baris pra-P0-4.
	UnitID               uint64 `gorm:"not null;default:0"                           json:"unit_id"`
	UnitNameSnapshot     string `gorm:"size:100;not null;default:''"                 json:"unit_name_snapshot"`
	AccountingClass      string `gorm:"not null;size:20"                             json:"accounting_class"`
	InventoryAccountCode string `gorm:"not null;size:20"                             json:"inventory_account_code"`
	// Bukti basis alokasi (audit trail, didenormalisasi agar tiap baris berdiri
	// sendiri). Sama untuk baris hard/soft/financing dalam satu snapshot; baris
	// land BERBEDA (basis_type="land_area" — Item 9, UAT 2026-09-07; sebelumnya
	// "equal"/selalu rata per rule klien UAT #1, digantikan Item 9):
	//   BasisType            : saleable_area | sales_value | land_area (khusus land)
	//   BasisValue           : bobot unit ini (sqm/rupiah list_price, atau land_area m2 untuk land)
	//   AllocationPercentage : porsi unit thd TOTAL basis, 0..100 (BasisValue/Σ×100)
	// Relasi: Amount ≈ pool_kelas × AllocationPercentage% (deviasi ≤ Rp1 karena
	// largest-remainder; Amount = nilai ACTUAL teralokasi) — berlaku juga untuk
	// land karena baris ini memakai basis land-nya sendiri, bukan basis proyek.
	BasisType            string          `gorm:"not null;size:20"                              json:"basis_type"`
	BasisValue           decimal.Decimal `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"  json:"basis_value"`
	AllocationPercentage decimal.Decimal `gorm:"type:DECIMAL(9,6);not null;default:'0.000000'" json:"allocation_percentage"`
	Amount               domain.Money    `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'"  json:"amount"`
	CreatedAt            time.Time       `                                                     json:"created_at"`
	UpdatedAt            time.Time       `                                                     json:"updated_at"`
}

func (AllocationSnapshotLine) TableName() string { return "allocation_snapshot_lines" }

// SnapshotDraft adalah data snapshot yang sudah dihitung namun BELUM dipersist.
// Dibuat oleh HPPResolver (di luar transaksi) lalu ditulis atomik di dalam
// transaksi BAST (BASTAtomicWriter.Execute) bersama jurnal + sale_record.
// Membangun baris dari domain.AllCostCategories — tidak ada string kategori
// yang di-hardcode (taxonomy = source of truth).
type SnapshotDraft struct {
	ProjectID         uint64
	PhaseID           *uint64
	UnitID            uint64
	BudgetPlanID      uint64
	BudgetPlanVersion int
	Basis             string // basis_type: saleable_area | sales_value
	// P0-4 D1: pin version basis alokasi (nil = source version tak tersedia —
	// snapshot tetap sah, diperlakukan legacy).
	ConfigVersionID *uint64
	ConfigVersion   *int
	// Bukti basis unit ini untuk kelas Hard/Soft/Financing (dipetakan ke baris
	// snapshot ketiga kelas itu):
	BasisValue           decimal.Decimal // bobot unit (sqm / rupiah list_price)
	AllocationPercentage decimal.Decimal // porsi thd total basis, 0..100
	// Bukti basis unit ini KHUSUS baris Land — land_area unit itu (Item 9, UAT
	// 2026-09-07; sebelumnya selalu rata per rule klien UAT #1), dicatat
	// terpisah dari BasisValue/AllocationPercentage di atas supaya baris Land
	// tidak salah mengklaim memakai basis Hard/Soft/Financing.
	LandBasisValue           decimal.Decimal // land_area unit ini (m2)
	LandAllocationPercentage decimal.Decimal // LandBasisValue/Σland_area unit properti × 100
	Breakdown                domain.UnitCostBreakdown
}

// landBasisTypeArea adalah basis_type baris snapshot Land — bukan bagian dari
// allocation.AllocationBasis (saleable_area/sales_value) karena Land punya
// basisnya sendiri: land_area per unit (Item 9, UAT 2026-09-07 — sebelumnya
// "equal"/selalu rata per rule klien UAT #1, dicabut klien).
const landBasisTypeArea = "land_area"

// toSnapshot membangun AllocationSnapshot + baris per accounting_class dari
// draft, memakai taxonomy sebagai satu-satunya sumber pemetaan class→akun.
// unitName = kode unit SAAT BAST (identitas historis P0-4 Req#1).
func (d SnapshotDraft) toSnapshot(tenantID uint64, unitName string) *AllocationSnapshot {
	snap := &AllocationSnapshot{
		TenantID:                  tenantID,
		ProjectID:                 d.ProjectID,
		PhaseID:                   d.PhaseID,
		UnitID:                    d.UnitID,
		BudgetPlanID:              d.BudgetPlanID,
		BudgetPlanVersion:         d.BudgetPlanVersion,
		Basis:                     d.Basis,
		AllocationConfigVersionID: d.ConfigVersionID,
		AllocationConfigVersion:   d.ConfigVersion,
	}
	hppTotal := domain.Zero
	for _, class := range domain.AllCostCategories {
		amount := d.Breakdown.Amount(class)
		line := AllocationSnapshotLine{
			TenantID:             tenantID,
			UnitID:               d.UnitID,
			UnitNameSnapshot:     unitName,
			AccountingClass:      string(class),
			InventoryAccountCode: class.InventoryAccountCode(),
			BasisType:            d.Basis,
			BasisValue:           d.BasisValue,
			AllocationPercentage: d.AllocationPercentage,
			Amount:               amount,
		}
		// Land punya basis sendiri: land_area per unit (Item 9) — basis_type/
		// value/pct baris ini HARUS mencerminkan itu, bukan basis Hard/Soft,
		// supaya "Amount ≈ pool_kelas × pct%" tetap benar untuk auditor.
		if class == domain.CostCategoryLand {
			line.BasisType = landBasisTypeArea
			line.BasisValue = d.LandBasisValue
			line.AllocationPercentage = d.LandAllocationPercentage
		}
		snap.Lines = append(snap.Lines, line)
		hppTotal = hppTotal.Add(amount)
	}
	// HPPTotal SENGAJA dijumlahkan dari Lines (bukan d.Breakdown.Total(), yang
	// masih menjumlahkan field legacy Financing) — menjamin Σ Lines == HPPTotal
	// selalu benar by construction, bukan kebetulan karena Financing selalu Zero
	// untuk plan baru (RULE KLIEN 2026-09-04).
	snap.HPPTotal = hppTotal
	return snap
}
