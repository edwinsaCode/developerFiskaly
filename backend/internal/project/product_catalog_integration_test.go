//go:build integration

package project_test

// H-2 — validasi mapping akun pendapatan produk (real MySQL).
//
// Mapping akun produk adalah jalur uang: apa pun yang dipetakan di sini akan
// dikredit saat BAST. Karena itu ia hanya boleh menunjuk akun PENDAPATAN yang
// ada dan aktif di COA tenant. Mapping ke Aset/Liabilitas/Ekuitas/Beban akan
// menghasilkan jurnal yang tetap balanced tapi SALAH secara akuntansi — laba
// rugi dan neraca ikut salah tanpa ada yang error. Karena itu: fail-closed di
// titik konfigurasi, jauh sebelum ada transaksi.

import (
	"context"
	"errors"
	"testing"

	"esaproperti/internal/project"
)

func TestIntegration_RevenueAccountMapping_FailsClosed(t *testing.T) {
	db := ubConnect(t)
	ubCleanup(t, db)
	defer ubCleanup(t, db)
	ctx := context.Background()
	svc, _ := ubSetup(t, db)
	repo := project.NewGORMRepository(db)

	// COA standar sudah ter-seed oleh ubSetup. Tambah satu akun revenue nonaktif
	// untuk menguji cabang "ada tapi nonaktif".
	if err := db.Exec(`INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_active)
		VALUES (?,?,?,?,?,FALSE)`, ubTenant, "4-9999", "Pendapatan Ditutup", "revenue", "credit").Error; err != nil {
		t.Fatalf("seed akun nonaktif: %v", err)
	}

	cases := []struct {
		name string
		code string
	}{
		{"akun_aset_ditolak", "1-1000"},       // Kas
		{"akun_liabilitas_ditolak", "2-2000"}, // Uang Muka Penjualan
		{"akun_beban_ditolak", "5-1000"},      // HPP
		{"akun_tidak_ada_ditolak", "9-9999"},  //  bukan bagian COA
		{"akun_nonaktif_ditolak", "4-9999"},   // revenue tapi nonaktif
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if err := repo.ValidateRevenueAccount(ctx, ubTenant, tc.code); !errors.Is(err, project.ErrRevenueAccountInvalid) {
				t.Fatalf("ValidateRevenueAccount(%q): want ErrRevenueAccountInvalid, got %v", tc.code, err)
			}
			// Jalur service (yang dipakai UI) menolak dengan error yang sama —
			// mapping salah tidak pernah tersimpan.
			if _, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
				Code: "gudang", Name: "Gudang", Category: project.ProductCategoryProperty,
				RevenueAccountCode: tc.code,
			}); !errors.Is(err, project.ErrRevenueAccountInvalid) {
				t.Fatalf("CreateProductType(%q): want ErrRevenueAccountInvalid, got %v", tc.code, err)
			}
			var n int64
			db.Raw(`SELECT COUNT(*) FROM product_types WHERE tenant_id = ? AND code = 'gudang'`, ubTenant).Scan(&n)
			if n != 0 {
				t.Errorf("product type tersimpan padahal mapping akun tidak sah")
			}
		})
	}

	// Kode akun kosong ditolak di level repository (tidak ada "akun tanpa nama").
	if err := repo.ValidateRevenueAccount(ctx, ubTenant, "   "); !errors.Is(err, project.ErrRevenueAccountInvalid) {
		t.Errorf("kode kosong: want ErrRevenueAccountInvalid, got %v", err)
	}
	// Di level service, mapping yang tidak diisi berarti "pakai default properti"
	// (4-1000) — default itu sendiri tetap divalidasi terhadap COA, jadi bukan
	// fail-open: kalau 4-1000 tidak ada/nonaktif, pembuatan produk gagal.
	def, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "rumah", Name: "Rumah", Category: project.ProductCategoryProperty,
	})
	if err != nil {
		t.Fatalf("CreateProductType tanpa mapping: %v", err)
	}
	if def.RevenueAccountCode != "4-1000" {
		t.Errorf("default mapping = %q, want 4-1000", def.RevenueAccountCode)
	}

	// Akun revenue yang sah diterima (4-1100 = Pendapatan Kelebihan Tanah,
	// bukan 4-2000/4-2100 yang reserved untuk Pendapatan Luar Usaha/Booking).
	pt, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "kelebihan_tanah", Name: "Kelebihan Tanah",
		Category: project.ProductCategoryNonProperty, RevenueAccountCode: "4-1100",
	})
	if err != nil {
		t.Fatalf("CreateProductType akun sah: %v", err)
	}

	// Update mapping tunduk pada aturan yang sama (bukan hanya saat dibuat).
	bad := "1-1000"
	if _, err := svc.UpdateProductType(ctx, ubTenant, pt.ID, nil, &bad, nil); !errors.Is(err, project.ErrRevenueAccountInvalid) {
		t.Fatalf("UpdateProductType ke akun aset: want ErrRevenueAccountInvalid, got %v", err)
	}
	var stored string
	db.Raw(`SELECT revenue_account_code FROM product_types WHERE id = ?`, pt.ID).Scan(&stored)
	if stored != "4-1100" {
		t.Errorf("mapping berubah menjadi %q padahal update ditolak", stored)
	}

	// Akun reserved (RoleOtherIncome/RoleBookingRevenue) ditolak untuk katalog
	// produk — mencegah pendapatan penjualan salah klasifikasi jadi Luar Usaha.
	for _, reserved := range []string{"4-2000", "4-2100"} {
		if _, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
			Code: "produk-" + reserved, Name: "Produk " + reserved,
			Category: project.ProductCategoryNonProperty, RevenueAccountCode: reserved,
		}); !errors.Is(err, project.ErrRevenueAccountInvalid) {
			t.Errorf("CreateProductType akun reserved %q: want ErrRevenueAccountInvalid, got %v", reserved, err)
		}
	}
}

// Produk yang dinonaktifkan TIDAK boleh dipakai memproses transaksi baru:
// resolver menolaknya, bukan mengabaikan flag-nya.
func TestIntegration_InactiveProduct_ResolverFailsClosed(t *testing.T) {
	db := ubConnect(t)
	ubCleanup(t, db)
	defer ubCleanup(t, db)
	ctx := context.Background()
	svc, _ := ubSetup(t, db)
	repo := project.NewGORMRepository(db)

	pt, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "ruko", Name: "Ruko", Category: project.ProductCategoryProperty,
	})
	if err != nil {
		t.Fatalf("CreateProductType: %v", err)
	}
	if _, err := repo.ResolveProductPolicy(ctx, ubTenant, "ruko"); err != nil {
		t.Fatalf("produk aktif harus resolve: %v", err)
	}

	inactive := false
	if _, err := svc.UpdateProductType(ctx, ubTenant, pt.ID, nil, nil, &inactive); err != nil {
		t.Fatalf("nonaktifkan produk: %v", err)
	}
	if _, err := repo.ResolveProductPolicy(ctx, ubTenant, "ruko"); !errors.Is(err, project.ErrProductTypeInactive) {
		t.Fatalf("produk nonaktif: want ErrProductTypeInactive, got %v", err)
	}
}

// Isolasi tenant (Invariant #6): katalog tenant lain tidak pernah terlihat.
func TestIntegration_ProductPolicy_TenantIsolated(t *testing.T) {
	db := ubConnect(t)
	ubCleanup(t, db)
	defer ubCleanup(t, db)
	ctx := context.Background()
	svc, _ := ubSetup(t, db)
	repo := project.NewGORMRepository(db)

	if _, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "kavling", Name: "Kavling", Category: project.ProductCategoryProperty,
	}); err != nil {
		t.Fatalf("CreateProductType: %v", err)
	}
	const otherTenant = ubTenant + 1
	if _, err := repo.ResolveProductPolicy(ctx, otherTenant, "kavling"); !errors.Is(err, project.ErrUnitTypeUnknown) {
		t.Fatalf("katalog bocor lintas tenant: got %v", err)
	}
}
