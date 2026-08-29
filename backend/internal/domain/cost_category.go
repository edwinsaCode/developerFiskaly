package domain

// CostCategory classifies a development cost for capitalization routing.
// Each category maps to a distinct Persediaan Real Estat sub-account (1-3xxx).
//
// Phase 5: the posting engine uses CostCategory.InventoryAccountCode() to determine
// the Dr account when posting Event 1 (kapitalisasi biaya pengembangan).
// Phase 6: the allocation engine groups CostEntry records by category before
// distributing project-level costs across units.
type CostCategory string

const (
	// CostCategoryLand covers land acquisition costs → akun 1-3000.
	CostCategoryLand CostCategory = "land"
	// CostCategoryHard covers construction/hard costs → akun 1-3100.
	CostCategoryHard CostCategory = "hard"
	// CostCategorySoft covers design, permits, legal fees → akun 1-3200.
	CostCategorySoft CostCategory = "soft"
	// CostCategoryFinancing covers capitalized interest and loan fees → akun 1-3300.
	CostCategoryFinancing CostCategory = "financing"

	// Kategori BEBAN (period expense) — hanya sah untuk CostTier overhead.
	// TIDAK PERNAH dikapitalisasi ke Persediaan, TIDAK PERNAH masuk pool HPP
	// (budgeted-cost-allocation-spec.md §3). Increment 2 (Cost 3-tier).

	// CostCategoryMarketing covers marketing/promotion spend → akun 5-3000 Beban Pemasaran.
	CostCategoryMarketing CostCategory = "marketing"
	// CostCategoryOther covers general & administrative spend → akun 5-4000 Beban Umum & Administrasi.
	CostCategoryOther CostCategory = "other"
)

// AllCostCategories is the canonical, ordered list of CAPITALIZABLE cost classes
// (the HPP accounting-class taxonomy). It deliberately EXCLUDES the expense-only
// categories (marketing/other) — allocation snapshots, HPP pools, and per-class
// journal lines must keep iterating exactly these four (BCA-1).
// This is the SINGLE SOURCE OF TRUTH for the accounting-class taxonomy — iterate
// this instead of hardcoding category strings when building per-class journal
// lines, allocation snapshots, or reports.
var AllCostCategories = []CostCategory{
	CostCategoryLand, CostCategoryHard, CostCategorySoft, CostCategoryFinancing,
}

// ExpenseCostCategories is the canonical, ordered list of expense-only (period
// cost) categories. Companion of AllCostCategories; the two sets are disjoint.
var ExpenseCostCategories = []CostCategory{
	CostCategoryMarketing, CostCategoryOther,
}

// Valid returns true if c is a recognized CostCategory (capitalizable OR expense).
// Tier-compatibility (which tier may use which category) is CostTier.AllowsCategory.
func (c CostCategory) Valid() bool {
	return c.IsCapitalizable() || c.IsExpense()
}

// IsCapitalizable returns true untuk kategori yang dikapitalisasi ke Persediaan
// Real Estat (1-3xxx) dan masuk pool HPP: land|hard|soft|financing.
func (c CostCategory) IsCapitalizable() bool {
	switch c {
	case CostCategoryLand, CostCategoryHard, CostCategorySoft, CostCategoryFinancing:
		return true
	}
	return false
}

// IsExpense returns true untuk kategori beban periode: marketing|other.
func (c CostCategory) IsExpense() bool {
	return c == CostCategoryMarketing || c == CostCategoryOther
}

// ExpenseAccountCode returns akun beban P&L (Dr side) untuk kategori expense-only.
// PEMETAAN TUNGGAL & TERPUSAT (freeze budgeted-cost-allocation-spec.md §3) —
// budget.BudgetCategory.ExpenseAccountCode mendelegasikan ke sini; jangan
// hardcode akun beban di tempat lain. Kategori kapitalisasi mengembalikan ""
// (mereka lewat InventoryAccountCode ke Persediaan).
func (c CostCategory) ExpenseAccountCode() string {
	switch c {
	case CostCategoryMarketing:
		return "5-3000" // Beban Pemasaran
	case CostCategoryOther:
		return "5-4000" // Beban Umum & Administrasi
	}
	return ""
}

// InventoryAccountCode returns the Persediaan Real Estat sub-account code
// that costs in this category should be posted to (Dr side, posting-rules.md Event 1).
// Phase 5: posting engine calls this to route each CostEntry to the correct account.
func (c CostCategory) InventoryAccountCode() string {
	switch c {
	case CostCategoryLand:
		return "1-3000"
	case CostCategoryHard:
		return "1-3100"
	case CostCategorySoft:
		return "1-3200"
	case CostCategoryFinancing:
		return "1-3300"
	}
	return ""
}

// UnitCostBreakdown holds a unit's accumulated development cost split by category.
//
// Phase 5: the cost allocation engine populates this from CostEntry records tagged
// to the unit (direct costs) plus the unit's proportional share of project-level costs.
// Phase 7: breakdown.Total() is the HPP basis for Event 4 (posting-rules.md) when
// the unit is sold — it must equal the sum credited from Persediaan Real Estat.
type UnitCostBreakdown struct {
	Land      Money // porsi biaya tanah
	Hard      Money // porsi biaya konstruksi
	Soft      Money // porsi biaya perizinan/desain/legal
	Financing Money // porsi bunga/biaya pinjaman dikapitalisasi
}

// Total returns the sum of all category costs (= HPP saat unit terjual).
// Phase 7: this value drives the Dr HPP / Cr Persediaan entries in Event 4.
func (b UnitCostBreakdown) Total() Money {
	return b.Land.Add(b.Hard).Add(b.Soft).Add(b.Financing)
}

// Amount returns the breakdown's Money for a given cost class, or Zero for an
// unrecognized category. Lets callers walk AllCostCategories without hardcoding
// the field-per-class mapping (taxonomy = source of truth).
func (b UnitCostBreakdown) Amount(c CostCategory) Money {
	switch c {
	case CostCategoryLand:
		return b.Land
	case CostCategoryHard:
		return b.Hard
	case CostCategorySoft:
		return b.Soft
	case CostCategoryFinancing:
		return b.Financing
	}
	return Zero
}
