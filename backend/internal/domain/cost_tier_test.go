package domain_test

import (
	"testing"

	"esaproperti/internal/domain"
)

func TestCostTier_Valid(t *testing.T) {
	for _, tier := range domain.AllCostTiers {
		if !tier.Valid() {
			t.Errorf("CostTier %q should be valid", tier)
		}
	}
	for _, tier := range []domain.CostTier{"", "unknown", "Direct", "OVERHEAD"} {
		if tier.Valid() {
			t.Errorf("CostTier %q should NOT be valid", tier)
		}
	}
}

func TestCostTier_Capitalizes(t *testing.T) {
	cases := map[domain.CostTier]bool{
		domain.CostTierDirect:   true,
		domain.CostTierShared:   true,
		domain.CostTierOverhead: false,
	}
	for tier, want := range cases {
		if got := tier.Capitalizes(); got != want {
			t.Errorf("CostTier(%q).Capitalizes() = %v, want %v", tier, got, want)
		}
	}
}

// TestCostTier_AllowsCategory_FullMatrix menguji SELURUH matriks tier × kategori:
// direct/shared hanya kategori kapitalisasi; overhead hanya kategori beban.
func TestCostTier_AllowsCategory_FullMatrix(t *testing.T) {
	capitalizable := domain.AllCostCategories    // land, hard, soft, financing
	expense := domain.ExpenseCostCategories      // marketing, other

	for _, tier := range []domain.CostTier{domain.CostTierDirect, domain.CostTierShared} {
		for _, c := range capitalizable {
			if !tier.AllowsCategory(c) {
				t.Errorf("%s + %s harus diizinkan", tier, c)
			}
		}
		for _, c := range expense {
			if tier.AllowsCategory(c) {
				t.Errorf("%s + %s harus DITOLAK (beban tidak boleh dikapitalisasi)", tier, c)
			}
		}
	}

	for _, c := range expense {
		if !domain.CostTierOverhead.AllowsCategory(c) {
			t.Errorf("overhead + %s harus diizinkan", c)
		}
	}
	for _, c := range capitalizable {
		if domain.CostTierOverhead.AllowsCategory(c) {
			t.Errorf("overhead + %s harus DITOLAK (kapitalisasi tidak boleh jadi beban)", c)
		}
	}

	// Tier/kategori tak dikenal → selalu false.
	if domain.CostTier("unknown").AllowsCategory(domain.CostCategoryLand) {
		t.Error("tier tak dikenal harus menolak semua kategori")
	}
	if domain.CostTierDirect.AllowsCategory("unknown") {
		t.Error("kategori tak dikenal harus ditolak semua tier")
	}
}
