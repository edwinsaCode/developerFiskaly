package ledger

import (
	"testing"

	"esaproperti/internal/domain"
)

// seedCategoryFor mengklasifikasikan akun bawaan saat seeding.
func TestSeedCategoryFor(t *testing.T) {
	cases := []struct {
		code string
		typ  domain.AccountType
		want AccountCategory
	}{
		{"1-1100", domain.AccountAsset, CategoryCash},
		{"1-1200", domain.AccountAsset, CategoryCash},
		{"1-1300", domain.AccountAsset, CategoryBank},
		{"1-1500", domain.AccountAsset, CategoryBank},
		{"1-2000", domain.AccountAsset, CategoryOtherAsset}, // Piutang
		{"1-3100", domain.AccountAsset, CategoryOtherAsset}, // Persediaan
		{"2-1000", domain.AccountLiability, ""},             // non-aset
		{"4-1000", domain.AccountRevenue, ""},
	}
	for _, c := range cases {
		if got := seedCategoryFor(c.code, c.typ); got != c.want {
			t.Errorf("seedCategoryFor(%s,%s)=%s, want %s", c.code, c.typ, got, c.want)
		}
	}
}
