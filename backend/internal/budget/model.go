package budget

import (
	"time"

	"github.com/shopspring/decimal"

	"esaproperti/internal/domain"
)

// ── BudgetCategory ────────────────────────────────────────────────────────────

// BudgetCategory classifies a RAB line item.
// RAB menggunakan 6 kategori; hanya 4 yang bisa dikapitalisasi ke Persediaan.
//
// 'construction' adalah ALIAS dari domain.CostCategoryHard.
// Jangan rename CostCategoryHard — cukup petakan saat membandingkan RAB vs realisasi.
//
// 'marketing' dan 'other' adalah expense-only (di-expense, tidak dikapitalisasi ke
// Persediaan Real Estat, tidak masuk HPP).
type BudgetCategory string

const (
	BudgetCategoryLand         BudgetCategory = "land"
	BudgetCategoryConstruction BudgetCategory = "construction" // alias: domain.CostCategoryHard
	BudgetCategorySoft         BudgetCategory = "soft"
	BudgetCategoryFinancing    BudgetCategory = "financing"
	BudgetCategoryMarketing    BudgetCategory = "marketing" // expense-only, tidak ada akun Persediaan
	BudgetCategoryOther        BudgetCategory = "other"     // expense-only, tidak ada akun Persediaan
)

// AllBudgetCategories adalah urutan tetap kategori untuk laporan RAB vs Realisasi.
var AllBudgetCategories = []BudgetCategory{
	BudgetCategoryLand, BudgetCategoryConstruction,
	BudgetCategorySoft, BudgetCategoryFinancing,
	BudgetCategoryMarketing, BudgetCategoryOther,
}

// Valid returns true jika c adalah BudgetCategory yang dikenali.
func (c BudgetCategory) Valid() bool {
	for _, bc := range AllBudgetCategories {
		if c == bc {
			return true
		}
	}
	return false
}

// ToCostCategory memetakan BudgetCategory ke domain.CostCategory KAPITALISASI.
// Returns ("", false) untuk marketing dan other (tidak dikapitalisasi — dipakai
// sebagai gate pool HPP, lihat GetBudgetedHPPBasis). Untuk pemetaan realisasi
// penuh (termasuk kategori beban) gunakan CostEntryCategory.
// construction ↔ hard adalah satu-satunya alias; keempat lainnya name-identical.
func (c BudgetCategory) ToCostCategory() (domain.CostCategory, bool) {
	switch c {
	case BudgetCategoryLand:
		return domain.CostCategoryLand, true
	case BudgetCategoryConstruction:
		return domain.CostCategoryHard, true // alias: construction = hard
	case BudgetCategorySoft:
		return domain.CostCategorySoft, true
	case BudgetCategoryFinancing:
		return domain.CostCategoryFinancing, true
	}
	return "", false // marketing dan other: tidak dikapitalisasi
}

// CostEntryCategory memetakan BudgetCategory ke kategori cost_entries yang
// MEREALISASIKAN baris RAB itu — mencakup SEMUA 6 kategori (Increment 2):
// kategori kapitalisasi lewat ToCostCategory (construction→hard), kategori
// beban ke padanan expense-nya (marketing→marketing, other→other; hanya sah
// untuk CostTier overhead). Returns ("", false) hanya untuk kategori tak dikenal.
func (c BudgetCategory) CostEntryCategory() (domain.CostCategory, bool) {
	if cc, ok := c.ToCostCategory(); ok {
		return cc, true
	}
	switch c {
	case BudgetCategoryMarketing:
		return domain.CostCategoryMarketing, true
	case BudgetCategoryOther:
		return domain.CostCategoryOther, true
	}
	return "", false
}

// IsExpenseOnly returns true untuk kategori yang tidak dikapitalisasi ke Persediaan Real Estat.
func (c BudgetCategory) IsExpenseOnly() bool {
	return c == BudgetCategoryMarketing || c == BudgetCategoryOther
}

// IsCapitalized adalah kebalikan IsExpenseOnly: kategori yang MASUK HPP unit
// (land/construction/soft/financing → Persediaan Real Estat 1-3xxx). Sumber
// kebenaran tunggal pemisahan HPP-vs-Beban. Lihat docs/budgeted-cost-allocation-spec.md §3.
func (c BudgetCategory) IsCapitalized() bool {
	_, ok := c.ToCostCategory()
	return ok
}

// ExpenseAccountCode mengembalikan akun beban P&L untuk kategori non-kapitalisasi
// (marketing/other). Kategori kapitalisasi mengembalikan "" (mereka lewat
// ToCostCategory().InventoryAccountCode() ke Persediaan). Pemetaan kode akun
// beban SSOT-nya di domain.CostCategory.ExpenseAccountCode (Increment 2) —
// method ini hanya delegasi; jangan hardcode akun beban di tempat lain (freeze §3).
func (c BudgetCategory) ExpenseAccountCode() string {
	if cc, ok := c.CostEntryCategory(); ok {
		return cc.ExpenseAccountCode() // "" untuk kategori kapitalisasi
	}
	return ""
}

// ── BudgetPlanStatus ──────────────────────────────────────────────────────────

type BudgetPlanStatus string

const (
	BudgetPlanStatusDraft      BudgetPlanStatus = "draft"
	BudgetPlanStatusActive     BudgetPlanStatus = "active"
	BudgetPlanStatusSuperseded BudgetPlanStatus = "superseded"
)

func (s BudgetPlanStatus) Valid() bool {
	switch s {
	case BudgetPlanStatusDraft, BudgetPlanStatusActive, BudgetPlanStatusSuperseded:
		return true
	}
	return false
}

// ── BudgetPlan ────────────────────────────────────────────────────────────────

// BudgetPlan adalah versi berurutan dari Rencana Anggaran Biaya (RAB) suatu proyek/fase.
//
// INVARIANT #8: Hanya SATU BudgetPlan berstatus 'active' per (project_id, phase_id).
// Dijaga oleh DUA lapisan:
//  1. DB: UNIQUE INDEX (tenant_id, project_id, phase_id_key, active_key).
//     active_key = 'Y' saat active, NULL saat draft/superseded.
//     MySQL mengizinkan banyak NULL dalam UNIQUE — hanya satu 'Y' per kombinasi.
//     phase_id_key = 0 untuk project-level (phase_id IS NULL), else phase_id.
//     Ini mencegah race condition & direct DB write yang melanggar invariant.
//  2. App: ApproveAndSupersede menjaga active_key secara atomik dalam transaksi.
//
// Saat plan baru di-approve, plan lama otomatis menjadi 'superseded' (tidak dihapus).
// RAB TIDAK memposting jurnal apa pun. Ini hanya perencanaan.
type BudgetPlan struct {
	ID         uint64           `gorm:"primaryKey;autoIncrement"                      json:"id"`
	TenantID   uint64           `gorm:"not null;index"                                json:"-"`
	ProjectID  uint64           `gorm:"not null;index"                                json:"project_id"`
	PhaseID    *uint64          `gorm:"index"                                         json:"phase_id,omitempty"`
	PhaseIDKey uint64           `gorm:"not null;default:0"   json:"-"`
	ActiveKey  *string          `gorm:"size:1"               json:"-"`
	Version    int              `gorm:"not null;default:1"                            json:"version"`
	Label      string           `gorm:"not null;size:100"                             json:"label"`
	Status     BudgetPlanStatus `gorm:"not null;size:20;default:'draft'"              json:"status"`
	Notes      string           `gorm:"size:2000"                                     json:"notes,omitempty"`
	ApprovedAt *time.Time       `                                                     json:"approved_at,omitempty"`
	ApprovedBy *string          `gorm:"size:200"                                      json:"approved_by,omitempty"`
	CreatedAt  time.Time        `                                                     json:"created_at"`
	UpdatedAt  time.Time        `                                                     json:"updated_at"`
}

func (BudgetPlan) TableName() string { return "budget_plans" }

// ── BudgetItem ────────────────────────────────────────────────────────────────

// BudgetItem adalah satu baris anggaran dalam sebuah BudgetPlan.
// BudgetedAmount WAJIB rupiah bulat (Invariant #2).
type BudgetItem struct {
	ID             uint64         `gorm:"primaryKey;autoIncrement"                      json:"id"`
	TenantID       uint64         `gorm:"not null;index"                                json:"-"`
	BudgetPlanID   uint64         `gorm:"not null;index"                                json:"budget_plan_id"`
	Category       BudgetCategory `gorm:"not null;size:20"                              json:"category"`
	Subcategory    string         `gorm:"size:100"                                      json:"subcategory,omitempty"`
	Description    string         `gorm:"size:500"                                      json:"description,omitempty"`
	BudgetedAmount domain.Money   `gorm:"type:DECIMAL(20,4);not null;default:'0.0000'" json:"budgeted_amount"`
	CreatedAt      time.Time      `                                                     json:"created_at"`
	UpdatedAt      time.Time      `                                                     json:"updated_at"`
}

func (BudgetItem) TableName() string { return "budget_items" }

// ── Request types ─────────────────────────────────────────────────────────────

type CreatePlanRequest struct {
	ProjectID uint64
	PhaseID   *uint64
	Label     string
	Notes     string
}

type AddItemRequest struct {
	PlanID         uint64
	Category       BudgetCategory
	Subcategory    string
	Description    string
	BudgetedAmount domain.Money
}

type ApprovePlanRequest struct {
	PlanID     uint64
	ApprovedBy string
}

// ── Report types ──────────────────────────────────────────────────────────────

// RABvsRealisasiRow adalah satu baris dalam laporan RAB vs Realisasi.
// Selisih = Budgeted - Realisasi; negatif berarti over budget.
// PersenRealisasi = "N/A" jika budget = 0.
type RABvsRealisasiRow struct {
	Category        BudgetCategory `json:"category"`
	Budgeted        string         `json:"budgeted"`
	Realisasi       string         `json:"realisasi"`
	Selisih         string         `json:"selisih"`
	PersenRealisasi string         `json:"persen_realisasi"`
}

// ItemRealisasiRow adalah satu baris realisasi per budget item individual.
// Granularitas lebih halus dari RABvsRealisasiRow (per-kategori).
type ItemRealisasiRow struct {
	ItemID          uint64         `json:"item_id"`
	Category        BudgetCategory `json:"category"`
	Subcategory     string         `json:"subcategory,omitempty"`
	Description     string         `json:"description,omitempty"`
	Budgeted        string         `json:"budgeted"`
	Realisasi       string         `json:"realisasi"`
	Selisih         string         `json:"selisih"`
	PersenRealisasi string         `json:"persen_realisasi"`
}

// RABvsRealisasiReport adalah laporan perbandingan RAB vs Realisasi untuk plan active.
type RABvsRealisasiReport struct {
	PlanID         uint64              `json:"plan_id"`
	PlanLabel      string              `json:"plan_label"`
	PlanVersion    int                 `json:"plan_version"`
	ProjectID      uint64              `json:"project_id"`
	PhaseID        *uint64             `json:"phase_id,omitempty"`
	Rows           []RABvsRealisasiRow `json:"rows"`
	TotalBudgeted  string              `json:"total_budgeted"`
	TotalRealisasi string              `json:"total_realisasi"`
	TotalSelisih   string              `json:"total_selisih"`
	// S9/R-9 (additive): pemakaian anggaran dihitung BACKEND — FE display saja.
	// UsagePct = realisasi/budget × 100 (2 desimal, "0" bila budget 0).
	// Status: "over" (>100), "waspada" (>=80), "sehat" (<80).
	UsagePct string `json:"usage_pct"`
	Status   string `json:"status"`
}

// BudgetHealthStatus mengklasifikasikan pemakaian anggaran — SATU-SATUNYA
// tempat ambang 80/100 didefinisikan (dulu di-hardcode FE charts.tsx).
func BudgetHealthStatus(usagePct decimal.Decimal) string {
	switch {
	case usagePct.GreaterThan(decimal.NewFromInt(100)):
		return "over"
	case usagePct.GreaterThanOrEqual(decimal.NewFromInt(80)):
		return "waspada"
	default:
		return "sehat"
	}
}
