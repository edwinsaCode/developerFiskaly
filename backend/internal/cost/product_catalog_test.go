package cost_test

// Product Catalog — guard kapitalisasi biaya.
//
// Biaya tier `direct` mendebit Persediaan unit dan baru lepas menjadi HPP saat
// BAST unit itu. Produk NON-PROPERTI tidak pernah mengakui HPP, jadi biaya yang
// ditempel ke unit non-properti akan mengendap selamanya di neraca (persediaan
// hantu) — angka aset naik tanpa pernah menjadi beban. Guard ini menolaknya
// SEBELUM jurnal dibuat.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
)

// stubUnitPolicies memetakan unit_id → kebijakan produk (atau error).
type stubUnitPolicies struct {
	byUnit map[uint64]domain.ProductPolicy
	err    error
}

func (s stubUnitPolicies) ResolveUnitProductPolicy(_ context.Context, _, unitID uint64) (domain.ProductPolicy, error) {
	if s.err != nil {
		return domain.ProductPolicy{}, s.err
	}
	p, ok := s.byUnit[unitID]
	if !ok {
		return domain.ProductPolicy{}, errors.New("unit tidak ada di katalog")
	}
	return p, nil
}

func directReqForUnit(unitID uint64) cost.CreateCostEntryRequest {
	r := baseReq()
	r.CostTier = domain.CostTierDirect
	r.UnitID = ptr64(unitID)
	return r
}

func TestCost_DirectToNonPropertyUnit_Rejected(t *testing.T) {
	svc, _, writer, store, _ := defaultTestService()
	svc.SetUnitPolicyResolver(stubUnitPolicies{byUnit: map[uint64]domain.ProductPolicy{
		7: {Code: "rumah", Category: domain.ProductCategoryProperty, RevenueAccountCode: "4-1000"},
		9: {Code: "pdam", Category: domain.ProductCategoryNonProperty, RevenueAccountCode: "4-2000"},
	}})
	ctx := context.Background()

	// Unit properti → tetap boleh (perilaku lama tidak berubah).
	if _, err := svc.CreateCostEntry(ctx, 1, directReqForUnit(7)); err != nil {
		t.Fatalf("biaya direct ke unit properti seharusnya sah: %v", err)
	}

	// Unit non-properti → ditolak, dan TIDAK ada jurnal maupun cost entry.
	journalsBefore := len(writer.calls)
	entriesBefore := len(store.entries)
	if _, err := svc.CreateCostEntry(ctx, 1, directReqForUnit(9)); !errors.Is(err, cost.ErrUnitNotHPPEligible) {
		t.Fatalf("want ErrUnitNotHPPEligible, got %v", err)
	}
	if len(writer.calls) != journalsBefore {
		t.Error("jurnal terbentuk padahal request ditolak (harus fail-closed sebelum posting)")
	}
	if len(store.entries) != entriesBefore {
		t.Error("cost entry tersimpan padahal request ditolak")
	}

	// Preview memakai validasi yang sama — UI tidak boleh menampilkan pratinjau
	// jurnal untuk transaksi yang akan ditolak saat submit.
	if _, err := svc.PreviewCostEntry(ctx, 1, directReqForUnit(9)); !errors.Is(err, cost.ErrUnitNotHPPEligible) {
		t.Errorf("preview: want ErrUnitNotHPPEligible, got %v", err)
	}
}

func TestCost_UnresolvableUnitPolicy_FailsClosed(t *testing.T) {
	svc, _, writer, _, _ := defaultTestService()
	svc.SetUnitPolicyResolver(stubUnitPolicies{err: errors.New("koneksi DB terputus")})

	before := len(writer.calls)
	_, err := svc.CreateCostEntry(context.Background(), 1, directReqForUnit(7))
	if !errors.Is(err, cost.ErrUnitProductPolicyUnresolved) {
		t.Fatalf("want ErrUnitProductPolicyUnresolved, got %v", err)
	}
	if len(writer.calls) != before {
		t.Error("kegagalan resolver tidak boleh berujung jurnal (no fail-open)")
	}
}

// Tier shared/overhead tidak menyentuh unit sama sekali, jadi guard tidak boleh
// ikut campur (dan tidak boleh memanggil resolver dengan unit nil).
func TestCost_SharedTier_UnaffectedByCatalogGuard(t *testing.T) {
	svc, _, _, _, _ := defaultTestService()
	svc.SetUnitPolicyResolver(stubUnitPolicies{err: errors.New("resolver tidak boleh dipanggil")})

	req := baseReq()
	req.CostTier = domain.CostTierShared
	req.UnitID = nil
	if _, err := svc.CreateCostEntry(context.Background(), 1, req); err != nil {
		t.Fatalf("biaya shared seharusnya tidak menyentuh katalog: %v", err)
	}
}
