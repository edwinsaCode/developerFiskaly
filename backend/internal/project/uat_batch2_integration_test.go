//go:build integration

package project_test

// UAT Batch 2 — integration: §2 Product Catalog (master + validasi unit_type)
// dan §4 Bulk Unit Generator (atomik, duplikat = batal semua).

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/shopspring/decimal"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
)

const ubTenant uint64 = 9_900_042

func ubConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func ubCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Urutan menghormati FK: units/project_phases sebelum projects.
	for _, tbl := range []string{"units", "product_types", "project_phases", "projects", "accounts"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", ubTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func ubSetup(t *testing.T, db *gorm.DB) (*project.Service, uint64) {
	t.Helper()
	repo := project.NewGORMRepository(db)
	svc := project.NewService(repo, repo, repo)
	svc.SetProductTypeStore(repo)

	// H-2: mapping akun pendapatan divalidasi terhadap COA nyata — fixture wajib
	// punya COA (bukan lagi string bebas).
	if err := ledger.SeedCOA(context.Background(), db, ubTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	// Akun 4-2200 dibuat KHUSUS untuk test ini (bukan bagian COA produksi) —
	// sengaja TIDAK pakai 4-2000: akun itu berperan RoleOtherIncome (Pendapatan
	// Luar Usaha), reserved sejak ValidateRevenueAccount di-hardening.
	if err := db.Exec(`INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_active, created_at, updated_at)
		VALUES (?,?,?,?,?,TRUE,NOW(3),NOW(3))`,
		ubTenant, "4-2200", "Pendapatan Produk Tambahan (test)", "revenue", "credit").Error; err != nil {
		t.Fatalf("seed akun 4-2200: %v", err)
	}

	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		ubTenant, "UAT Batch2", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	var pid uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&pid)
	return svc, pid
}

func TestIntegration_ProductCatalog_And_BulkUnits(t *testing.T) {
	db := ubConnect(t)
	ubCleanup(t, db)
	defer ubCleanup(t, db)
	ctx := context.Background()
	svc, projectID := ubSetup(t, db)

	// ── §2: master product type + mapping akun ───────────────────────────────
	rumah, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "rumah", Name: "Rumah", Category: project.ProductCategoryProperty,
	})
	if err != nil {
		t.Fatalf("CreateProductType rumah: %v", err)
	}
	if rumah.RevenueAccountCode != "4-1000" {
		t.Errorf("default revenue account = %s, want 4-1000", rumah.RevenueAccountCode)
	}
	if _, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "pdam", Name: "Sambungan PDAM", Category: project.ProductCategoryNonProperty,
		RevenueAccountCode: "4-2200",
	}); err != nil {
		t.Fatalf("CreateProductType pdam: %v", err)
	}
	// Duplikat kode ditolak.
	if _, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "rumah", Name: "Rumah 2", Category: project.ProductCategoryProperty,
	}); !errors.Is(err, project.ErrProductTypeDuplicate) {
		t.Errorf("duplikat product type: want ErrProductTypeDuplicate, got %v", err)
	}
	// Kebijakan produk per unit_type (seam BAST) — COA-driven, FAIL-CLOSED.
	// M-1: kode yang tidak terdaftar TIDAK lagi jatuh ke 4-1000 diam-diam.
	repo := project.NewGORMRepository(db)
	pdamPol, err := repo.ResolveProductPolicy(ctx, ubTenant, "pdam")
	if err != nil {
		t.Fatalf("ResolveProductPolicy pdam: %v", err)
	}
	if pdamPol.RevenueAccountCode != "4-2200" {
		t.Errorf("akun pdam = %s, want 4-2200", pdamPol.RevenueAccountCode)
	}
	if pdamPol.ParticipatesInHPP() {
		t.Error("pdam (non_property) tidak boleh ikut HPP")
	}
	if _, err := repo.ResolveProductPolicy(ctx, ubTenant, "villa-legacy"); !errors.Is(err, project.ErrUnitTypeUnknown) {
		t.Errorf("kode tak terdaftar: want ErrUnitTypeUnknown, got %v", err)
	}

	// unit_type tak terdaftar → ditolak (unit BARU wajib dari master).
	if _, err := svc.CreateUnit(ctx, ubTenant, project.CreateUnitRequest{
		ProjectID: projectID, Code: "X-01", UnitType: "hotel",
		SaleableArea: decimal.NewFromInt(70), ListPrice: domain.FromInt(100_000_000),
	}); !errors.Is(err, project.ErrUnitTypeUnknown) {
		t.Errorf("unit_type tak dikenal: want ErrUnitTypeUnknown, got %v", err)
	}

	// ── §4: bulk generator — Block A unit 1–12, Type 36, Land 72, harga sama ──
	label := "36/72"
	units, err := svc.BulkCreateUnits(ctx, ubTenant, project.BulkCreateUnitsRequest{
		ProjectID: projectID, Block: "A", UnitStart: 1, UnitEnd: 12,
		UnitType: "rumah", TypeLabel: &label,
		SaleableArea: decimal.NewFromInt(72), ListPrice: domain.FromInt(250_000_000),
	})
	if err != nil {
		t.Fatalf("BulkCreateUnits: %v", err)
	}
	if len(units) != 12 || units[0].Code != "A-01" || units[11].Code != "A-12" {
		t.Fatalf("bulk hasil = %d unit (%s..%s), want 12 A-01..A-12",
			len(units), units[0].Code, units[len(units)-1].Code)
	}
	var n int64
	db.Raw(`SELECT COUNT(*) FROM units WHERE tenant_id = ? AND project_id = ? AND type_label = '36/72'`,
		ubTenant, projectID).Scan(&n)
	if n != 12 {
		t.Errorf("type_label tersimpan utk %d unit, want 12", n)
	}

	// Duplikat (A-05 sudah ada) → SELURUH batch kedua batal (atomik).
	if _, err := svc.BulkCreateUnits(ctx, ubTenant, project.BulkCreateUnitsRequest{
		ProjectID: projectID, Block: "A", UnitStart: 5, UnitEnd: 20,
		UnitType: "rumah", SaleableArea: decimal.NewFromInt(72), ListPrice: domain.FromInt(250_000_000),
	}); !errors.Is(err, project.ErrBulkDuplicateCode) {
		t.Fatalf("bulk duplikat: want ErrBulkDuplicateCode, got %v", err)
	}
	db.Raw(`SELECT COUNT(*) FROM units WHERE tenant_id = ? AND project_id = ?`, ubTenant, projectID).Scan(&n)
	if n != 12 {
		t.Errorf("jumlah unit pasca-batch gagal = %d, want tetap 12 (tidak partial)", n)
	}

	// Rentang & cap dijaga eksplisit.
	if _, err := svc.BulkCreateUnits(ctx, ubTenant, project.BulkCreateUnitsRequest{
		ProjectID: projectID, Block: "B", UnitStart: 3, UnitEnd: 2,
		UnitType: "rumah", ListPrice: domain.FromInt(1_000_000),
	}); !errors.Is(err, project.ErrBulkRangeInvalid) {
		t.Errorf("rentang terbalik: want ErrBulkRangeInvalid, got %v", err)
	}
	if _, err := svc.BulkCreateUnits(ctx, ubTenant, project.BulkCreateUnitsRequest{
		ProjectID: projectID, Block: "C", UnitStart: 1, UnitEnd: 501,
		UnitType: "rumah", ListPrice: domain.FromInt(1_000_000),
	}); !errors.Is(err, project.ErrBulkTooMany) {
		t.Errorf("cap batch: want ErrBulkTooMany, got %v", err)
	}
}

// TestIntegration_ListUnits_ExcludesNonPropertyUnits (LT-0): regresi terhadap
// MySQL nyata, meniru persis bentuk data live yang memicu bug (tenant 9900246,
// unit 1714 & 5931: unit_type="kelebihan_tanah", terdaftar non_property di
// product_types). ListUnitsByProject/ListUnitsByPhase (dipakai /penjualan)
// tidak boleh lagi mengembalikan baris non-properti — tapi unit_type legacy
// yang belum terdaftar di master (banyak di tenant lama pra-katalog) harus
// tetap tampil, fail-open, bukan ikut tersaring.
func TestIntegration_ListUnits_ExcludesNonPropertyUnits(t *testing.T) {
	db := ubConnect(t)
	ubCleanup(t, db)
	defer ubCleanup(t, db)
	ctx := context.Background()
	svc, projectID := ubSetup(t, db)

	phase, err := svc.CreatePhase(ctx, ubTenant, project.CreatePhaseRequest{
		ProjectID: projectID, Name: "Fase 1",
	})
	if err != nil {
		t.Fatalf("CreatePhase: %v", err)
	}

	if _, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "rumah", Name: "Rumah", Category: project.ProductCategoryProperty,
	}); err != nil {
		t.Fatalf("CreateProductType rumah: %v", err)
	}
	if _, err := svc.CreateProductType(ctx, ubTenant, &project.ProductType{
		Code: "kelebihan_tanah", Name: "Kelebihan Tanah", Category: project.ProductCategoryNonProperty,
		RevenueAccountCode: "4-1100",
	}); err != nil {
		t.Fatalf("CreateProductType kelebihan_tanah: %v", err)
	}
	// "villa-legacy" sengaja TIDAK didaftarkan — mensimulasikan unit_type
	// pra-katalog tenant lama yang belum termapping ke master mana pun.

	if _, err := svc.CreateUnit(ctx, ubTenant, project.CreateUnitRequest{
		ProjectID: projectID, PhaseID: &phase.ID, Code: "R-01", UnitType: "rumah",
		SaleableArea: decimal.NewFromInt(72), ListPrice: domain.FromInt(250_000_000),
	}); err != nil {
		t.Fatalf("CreateUnit rumah: %v", err)
	}
	// Baris "kelebihan_tanah" di produksi (1714, 5931) sudah ada SEBELUM guard
	// W-13 (ErrUnitTypeNotProperty) berlaku — INSERT langsung mensimulasikan
	// data legacy itu apa adanya, bukan mencoba lewat jalur yang sekarang
	// menolaknya (dan seharusnya menolaknya — itu perilaku yang benar).
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, phase_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		ubTenant, projectID, phase.ID, "KT-01", "kelebihan_tanah", 100, 1_000_000, "available").Error; err != nil {
		t.Fatalf("seed legacy kelebihan_tanah unit: %v", err)
	}
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, phase_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		ubTenant, projectID, phase.ID, "V-LEGACY-01", "villa-legacy", 100, 1_000_000, "available").Error; err != nil {
		t.Fatalf("seed legacy villa-legacy unit: %v", err)
	}

	assertCodes := func(t *testing.T, list []*project.Unit, label string) {
		t.Helper()
		got := map[string]bool{}
		for _, u := range list {
			got[u.Code] = true
		}
		if !got["R-01"] {
			t.Errorf("%s: unit properti terdaftar (rumah) harus tetap tampil", label)
		}
		if !got["V-LEGACY-01"] {
			t.Errorf("%s: unit_type legacy belum termapping harus tetap tampil (fail-open)", label)
		}
		if got["KT-01"] {
			t.Errorf("%s: unit non-properti terdaftar (kelebihan_tanah) tidak boleh muncul di listing /penjualan", label)
		}
		if len(list) != 2 {
			t.Errorf("%s: jumlah unit di listing = %d, want 2", label, len(list))
		}
	}

	byProject, err := svc.ListUnitsByProject(ctx, ubTenant, projectID)
	if err != nil {
		t.Fatalf("ListUnitsByProject: %v", err)
	}
	assertCodes(t, byProject, "ListUnitsByProject")

	byPhase, err := svc.ListUnitsByPhase(ctx, ubTenant, phase.ID)
	if err != nil {
		t.Fatalf("ListUnitsByPhase: %v", err)
	}
	assertCodes(t, byPhase, "ListUnitsByPhase")
}
