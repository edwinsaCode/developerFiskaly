package charge

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/domain"
)

// LT-8 (docs/kelebihan-tanah-final-architecture-2026-08.md §G) — pembekuan
// jalur addon lama untuk produk berkategori `land`. Guard di resolveAddonProduct
// sudah generik sejak sebelum LT-8 (menolak ParticipatesInHPP()==true, ditulis
// untuk `property`); test ini membuktikan guard yang SAMA — tanpa perubahan
// kode — juga menolak `land` begitu master data mengklasifikasikannya begitu
// (migration 000083).

type stubProductPolicyResolver struct {
	pol domain.ProductPolicy
	err error
}

func (s stubProductPolicyResolver) ResolveProductPolicy(ctx context.Context, tenantID uint64, code string) (domain.ProductPolicy, error) {
	return s.pol, s.err
}

func TestResolveAddonProduct_RejectsLandCategory(t *testing.T) {
	svc := NewService(nil)
	svc.SetProductPolicyResolver(stubProductPolicyResolver{pol: domain.ProductPolicy{
		Code:               "kelebihan_tanah",
		Name:               "Kelebihan Tanah",
		Category:           domain.ProductCategoryLand,
		RevenueAccountCode: "4-1100",
	}})

	_, err := svc.resolveAddonProduct(context.Background(), 1, "kelebihan_tanah")
	if !errors.Is(err, ErrProductNotAddon) {
		t.Fatalf("produk kategori land harus ditolak sebagai addon (ErrProductNotAddon), dapat %v", err)
	}
}

func TestResolveAddonProduct_RejectsPropertyCategory(t *testing.T) {
	svc := NewService(nil)
	svc.SetProductPolicyResolver(stubProductPolicyResolver{pol: domain.ProductPolicy{
		Code:               "rumah",
		Name:               "Rumah",
		Category:           domain.ProductCategoryProperty,
		RevenueAccountCode: "4-1000",
	}})

	_, err := svc.resolveAddonProduct(context.Background(), 1, "rumah")
	if !errors.Is(err, ErrProductNotAddon) {
		t.Fatalf("produk kategori property harus ditolak sebagai addon (ErrProductNotAddon), dapat %v", err)
	}
}

func TestResolveAddonProduct_AcceptsNonPropertyCategory(t *testing.T) {
	svc := NewService(nil)
	svc.SetProductPolicyResolver(stubProductPolicyResolver{pol: domain.ProductPolicy{
		Code:               "pdam",
		Name:               "Sambungan PDAM",
		Category:           domain.ProductCategoryNonProperty,
		RevenueAccountCode: "4-2000",
	}})

	accs, err := svc.resolveAddonProduct(context.Background(), 1, "pdam")
	if err != nil {
		t.Fatalf("produk non_property harus tetap diterima sebagai addon: %v", err)
	}
	if accs.revenue != "4-2000" {
		t.Fatalf("akun pendapatan want 4-2000, got %s", accs.revenue)
	}
}
