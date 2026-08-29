package domain

import "testing"

func TestBillingTreatmentValid(t *testing.T) {
	cases := map[BillingTreatment]bool{
		TreatmentRevenue:          true,
		TreatmentDepositLiability: true,
		"":                        false,
		"titipan":                 false,
		"REVENUE":                 false,
	}
	for in, want := range cases {
		if got := in.Valid(); got != want {
			t.Fatalf("BillingTreatment(%q).Valid() = %v, mau %v", in, got, want)
		}
	}
}

// Fail-closed: perlakuan yang tidak dikenal TIDAK BOLEH menyentuh pendapatan.
func TestUnknownTreatmentNeverRecognizesRevenue(t *testing.T) {
	for _, in := range []BillingTreatment{"", "addon", "unknown", "Revenue"} {
		if in.RecognizesRevenue() {
			t.Fatalf("BillingTreatment(%q) tidak boleh mengakui pendapatan", in)
		}
		if in.IsThirdPartyDeposit() {
			t.Fatalf("BillingTreatment(%q) tidak boleh diperlakukan sebagai titipan", in)
		}
	}
}

func TestTreatmentsAreMutuallyExclusive(t *testing.T) {
	for _, tr := range AllBillingTreatments() {
		if tr.RecognizesRevenue() == tr.IsThirdPartyDeposit() {
			t.Fatalf("%q: pendapatan dan titipan tidak boleh bernilai sama", tr)
		}
	}
}

// Jembatan antar-master: produk selalu berperlakuan pendapatan.
func TestProductCategoryAlwaysRevenue(t *testing.T) {
	for _, c := range AllProductCategories() {
		if !c.BillingTreatment().RecognizesRevenue() {
			t.Fatalf("ProductCategory(%q) harus berperlakuan pendapatan", c)
		}
	}
}

func TestRealizationChargePolicyDelegates(t *testing.T) {
	p := RealizationChargePolicy{Code: "notaris", Treatment: TreatmentDepositLiability, DepositAccountCode: "2-2400"}
	if p.RecognizesRevenue() {
		t.Fatal("titipan realisasi tidak boleh mengakui pendapatan")
	}
	if !p.IsThirdPartyDeposit() {
		t.Fatal("titipan realisasi harus dikenali sebagai kewajiban pihak ketiga")
	}
}
