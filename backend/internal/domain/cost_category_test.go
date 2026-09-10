package domain_test

import (
	"testing"

	"esaproperti/internal/domain"
)

func TestCostCategory_Valid(t *testing.T) {
	// Increment 2: Valid() mencakup kategori kapitalisasi DAN kategori beban
	// (marketing/other) — kompatibilitas per-tier ada di CostTier.AllowsCategory.
	valid := []domain.CostCategory{
		domain.CostCategoryLand,
		domain.CostCategoryHard,
		domain.CostCategorySoft,
		domain.CostCategoryOperational,
		domain.CostCategoryMarketing,
		domain.CostCategoryOther,
	}
	for _, c := range valid {
		if !c.Valid() {
			t.Errorf("CostCategory %q should be valid", c)
		}
	}
	invalid := []domain.CostCategory{"", "unknown", "construction"}
	for _, c := range invalid {
		if c.Valid() {
			t.Errorf("CostCategory %q should NOT be valid", c)
		}
	}
}

func TestCostCategory_CapitalizableVsExpense_Disjoint(t *testing.T) {
	// AllCostCategories (pool HPP, BCA-1) tetap TEPAT 2 kategori kapitalisasi
	// (RULE KLIEN FREEZE 2026-09-04: HPP hanya Tanah + Konstruksi/Hard Cost —
	// Soft Cost menyusul Operasional keluar dari HPP).
	if len(domain.AllCostCategories) != 2 {
		t.Fatalf("AllCostCategories harus tetap 2 (taxonomy HPP), got %d", len(domain.AllCostCategories))
	}
	for _, c := range domain.AllCostCategories {
		if !c.IsCapitalizable() || c.IsExpense() {
			t.Errorf("%q harus capitalizable dan bukan expense", c)
		}
		if c.ExpenseAccountCode() != "" {
			t.Errorf("%q kategori kapitalisasi tidak boleh punya akun beban", c)
		}
	}
	for _, c := range domain.ExpenseCostCategories {
		if c.IsCapitalizable() || !c.IsExpense() {
			t.Errorf("%q harus expense dan bukan capitalizable", c)
		}
		if c.InventoryAccountCode() != "" {
			t.Errorf("%q kategori beban tidak boleh punya akun Persediaan", c)
		}
	}
}

func TestCostCategory_ExpenseAccountCode(t *testing.T) {
	cases := []struct {
		cat  domain.CostCategory
		want string
	}{
		{domain.CostCategoryMarketing, "5-3000"},
		{domain.CostCategoryOther, "5-4000"},
		{domain.CostCategoryOperational, "5-4600"},
		{domain.CostCategorySoft, "5-4700"},
	}
	for _, tc := range cases {
		if got := tc.cat.ExpenseAccountCode(); got != tc.want {
			t.Errorf("CostCategory(%q).ExpenseAccountCode() = %q, want %q", tc.cat, got, tc.want)
		}
	}
	if got := domain.CostCategory("unknown").ExpenseAccountCode(); got != "" {
		t.Errorf("unknown category should return empty expense account code, got %q", got)
	}
}

func TestCostCategory_InventoryAccountCode(t *testing.T) {
	cases := []struct {
		cat  domain.CostCategory
		want string
	}{
		{domain.CostCategoryLand, "1-3000"},
		{domain.CostCategoryHard, "1-3100"},
	}
	for _, tc := range cases {
		got := tc.cat.InventoryAccountCode()
		if got != tc.want {
			t.Errorf("CostCategory(%q).InventoryAccountCode() = %q, want %q", tc.cat, got, tc.want)
		}
	}
	// RULE KLIEN FREEZE (2026-09-04): Soft Cost bukan lagi kategori kapitalisasi
	// — InventoryAccountCode() harus kosong seperti kategori beban lainnya.
	if got := domain.CostCategorySoft.InventoryAccountCode(); got != "" {
		t.Errorf("CostCategorySoft.InventoryAccountCode() = %q, want empty (bukan HPP lagi)", got)
	}
	// Unknown category returns empty string.
	if got := domain.CostCategory("unknown").InventoryAccountCode(); got != "" {
		t.Errorf("unknown category should return empty account code, got %q", got)
	}
}

func TestUnitCostBreakdown_Total(t *testing.T) {
	b := domain.UnitCostBreakdown{
		Land:      domain.FromInt(100_000_000),
		Hard:      domain.FromInt(400_000_000),
		Soft:      domain.FromInt(50_000_000),
		Financing: domain.FromInt(50_000_000),
	}
	want := domain.FromInt(600_000_000)
	got := b.Total()
	if !got.Equal(want) {
		t.Errorf("UnitCostBreakdown.Total() = %s, want %s", got, want)
	}
}

func TestUnitCostBreakdown_Total_AllZero(t *testing.T) {
	b := domain.UnitCostBreakdown{}
	if !b.Total().Equal(domain.Zero) {
		t.Errorf("empty breakdown Total() should be zero, got %s", b.Total())
	}
}
