package domain

import "testing"

// Aturan kanonik partisipasi produk. Test ini menjaga agar tidak ada yang
// "melonggarkan" aturan diam-diam (mis. menganggap kategori tak dikenal ikut
// HPP) — itu akan mendilusi HPP rumah tanpa jejak.
func TestProductCategory_ParticipatesInHPP(t *testing.T) {
	cases := []struct {
		cat         ProductCategory
		wantHPP     bool
		wantProg    bool
		wantMayUnit bool
	}{
		{ProductCategoryProperty, true, true, true},
		{ProductCategoryLand, true, false, false},
		{ProductCategoryNonProperty, false, false, false},
		{ProductCategory(""), false, false, false},         // kosong = tidak dikenal
		{ProductCategory("hotel"), false, false, false},    // kategori masa depan
		{ProductCategory("PROPERTY"), false, false, false}, // case berbeda = tidak dikenal (kanonik lowercase)
	}
	for _, c := range cases {
		if got := c.cat.ParticipatesInHPP(); got != c.wantHPP {
			t.Errorf("%q.ParticipatesInHPP() = %v, want %v", c.cat, got, c.wantHPP)
		}
		if got := c.cat.ParticipatesInProjectProgress(); got != c.wantProg {
			t.Errorf("%q.ParticipatesInProjectProgress() = %v, want %v", c.cat, got, c.wantProg)
		}
		if got := c.cat.MayBecomeUnit(); got != c.wantMayUnit {
			t.Errorf("%q.MayBecomeUnit() = %v, want %v", c.cat, got, c.wantMayUnit)
		}
	}
}

func TestProductCategory_Valid(t *testing.T) {
	if !ProductCategoryProperty.Valid() || !ProductCategoryLand.Valid() || !ProductCategoryNonProperty.Valid() {
		t.Fatal("kategori baku harus valid")
	}
	if ProductCategory("gudang").Valid() {
		t.Error("kategori tak dikenal tidak boleh valid")
	}
}

// ParticipatesInCostPool: land HANYA ikut pool Land, property ikut semua pool
// kapitalisasi, non_property tidak ikut pool mana pun. Matriks ini adalah
// dasar allocation engine per-pool (LT-5) — dites di sini dulu sebagai
// kontrak domain, sebelum ada pemanggil nyata.
//
// RULE KLIEN FREEZE (2026-09-04): Soft Cost bukan lagi pool kapitalisasi —
// property TIDAK ikut pool Soft sekalipun produknya property (pool.IsCapitalizable()
// == false untuk Soft, lihat ParticipatesInCostPool).
func TestProductCategory_ParticipatesInCostPool(t *testing.T) {
	pools := []CostCategory{CostCategoryLand, CostCategoryHard, CostCategorySoft}
	cases := []struct {
		cat  ProductCategory
		want map[CostCategory]bool
	}{
		{ProductCategoryProperty, map[CostCategory]bool{
			CostCategoryLand: true, CostCategoryHard: true, CostCategorySoft: false,
		}},
		{ProductCategoryLand, map[CostCategory]bool{
			CostCategoryLand: true, CostCategoryHard: false, CostCategorySoft: false,
		}},
		{ProductCategoryNonProperty, map[CostCategory]bool{
			CostCategoryLand: false, CostCategoryHard: false, CostCategorySoft: false,
		}},
	}
	for _, c := range cases {
		for _, pool := range pools {
			if got := c.cat.ParticipatesInCostPool(pool); got != c.want[pool] {
				t.Errorf("%q.ParticipatesInCostPool(%q) = %v, want %v", c.cat, pool, got, c.want[pool])
			}
		}
	}
}

// HPPEligibleCategories WAJIB diturunkan dari fungsi kanonik, bukan daftar
// terpisah — kalau tidak, keduanya bisa berbeda kesimpulan (duplicate SoT).
func TestHPPEligibleCategories_DerivedFromCanonicalRule(t *testing.T) {
	got := HPPEligibleCategories()
	for _, c := range AllProductCategories() {
		want := c.ParticipatesInHPP()
		found := false
		for _, g := range got {
			if g == c {
				found = true
			}
		}
		if found != want {
			t.Errorf("kategori %q: ada di HPPEligibleCategories=%v, ParticipatesInHPP=%v", c, found, want)
		}
	}
}

func TestProductPolicy_DelegatesToCategory(t *testing.T) {
	p := ProductPolicy{Code: "pdam", Category: ProductCategoryNonProperty, RevenueAccountCode: "4-2000"}
	if p.ParticipatesInHPP() || p.ParticipatesInProjectProgress() {
		t.Error("produk non-properti tidak boleh ikut HPP maupun progress proyek")
	}
	h := ProductPolicy{Code: "rumah", Category: ProductCategoryProperty, RevenueAccountCode: "4-1000"}
	if !h.ParticipatesInHPP() || !h.ParticipatesInProjectProgress() {
		t.Error("produk properti harus ikut HPP dan progress proyek")
	}
}
