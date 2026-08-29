package ledger

import (
	"reflect"
	"testing"

	"esaproperti/internal/domain"
)

// Mengunci keanggotaan peran == nilai kanonik yang SEBELUMNYA hardcoded, agar
// migrasi S1 terbukti nol-perubahan dan tak ada yang menggeser diam-diam.
func TestAccountRole_CanonicalMembership(t *testing.T) {
	want := map[AccountRole][]string{
		RoleReceivable:          {"1-2000", "1-2100", "1-2200"},
		RolePayable:             {"2-1000"},
		RoleBookingLiability:    {"2-2100"},
		RoleCommissionLiability: {"2-6200"},
		RoleInventory:           {"1-3000", "1-3100", "1-3200", "1-3300"}, // == cost & allocation lama
		RoleVATOutput:           {"2-3000"},
		RoleVATInput:            {"1-5100"},
		RoleTaxLiability:        {"2-4000"},
	}
	for role, codes := range want {
		if got := RoleCodeList(role); !reflect.DeepEqual(got, codes) {
			t.Errorf("RoleCodeList(%s) = %v, want %v", role, got, codes)
		}
	}
}

// RoleCodeList mengembalikan salinan — mutasi caller tak merusak definisi.
func TestAccountRole_ListIsCopy(t *testing.T) {
	a := RoleCodeList(RoleInventory)
	if len(a) == 0 {
		t.Fatal("inventory kosong")
	}
	a[0] = "MUTATED"
	if RoleCodeList(RoleInventory)[0] == "MUTATED" {
		t.Error("RoleCodeList membocorkan slice internal (mutasi caller merusak registry)")
	}
}

func TestAccountRole_CashBankViaCategory(t *testing.T) {
	cash := &Account{Code: "1-1100", Type: domain.AccountAsset, Category: CategoryCash}
	bank := &Account{Code: "1-1300", Type: domain.AccountAsset, Category: CategoryBank}
	other := &Account{Code: "1-1900", Type: domain.AccountAsset, Category: CategoryOtherAsset}
	if !AccountInRole(cash, RoleCashBank) || !AccountInRole(bank, RoleCashBank) {
		t.Error("cash/bank category harus masuk RoleCashBank")
	}
	if AccountInRole(other, RoleCashBank) {
		t.Error("other_asset tidak boleh masuk RoleCashBank")
	}
	// Kode-based role:
	if !AccountInRole(&Account{Code: "1-3100"}, RoleInventory) {
		t.Error("1-3100 harus masuk RoleInventory")
	}
	if AccountInRole(&Account{Code: "1-2000"}, RoleInventory) {
		t.Error("1-2000 tidak boleh masuk RoleInventory")
	}
	if AccountInRole(nil, RoleReceivable) {
		t.Error("nil account tidak boleh match peran apa pun")
	}
}
