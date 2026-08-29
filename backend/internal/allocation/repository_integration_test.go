//go:build integration

package allocation_test

// H-1 — basis alokasi HPP dibaca langsung dari DB (real MySQL).
//
// GetUnitInputs adalah SATU-SATUNYA pintu masuk daftar unit ke engine alokasi
// (dipakai alokasi biaya aktual, resolver HPP budgeted saat BAST, dan true-up
// closing). Kalau pintu ini salah, tiga jalur uang sekaligus ikut salah — jadi
// perilakunya dikunci di sini terhadap DB sungguhan.

import (
	"context"
	"errors"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/allocation"
	"esaproperti/internal/domain"
)

const acTenant uint64 = 9_900_059

func acConnect(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN tidak di-set — lewati integration test")
	}
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("koneksi DB: %v", err)
	}
	return db
}

func acCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"units", "product_types", "projects"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", acTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func acSeed(t *testing.T, db *gorm.DB) uint64 {
	t.Helper()
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		acTenant, "Alokasi Katalog", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	var projectID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)

	for _, pt := range []struct{ code, category, acct string }{
		{"rumah", "property", "4-1000"},
		{"pdam", "non_property", "4-2000"},
	} {
		if err := db.Exec(`INSERT INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
			VALUES (?,?,?,?,?,TRUE)`, acTenant, pt.code, pt.code, pt.category, pt.acct).Error; err != nil {
			t.Fatalf("seed product type %s: %v", pt.code, err)
		}
	}
	return projectID
}

func acSeedUnit(t *testing.T, db *gorm.DB, projectID uint64, code, unitType string, area int64) uint64 {
	t.Helper()
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`,
		acTenant, projectID, code, unitType, domain.FromInt(area), domain.FromInt(0), "available").Error; err != nil {
		t.Fatalf("seed unit %s: %v", code, err)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func TestIntegration_GetUnitInputs_ExcludesNonProperty(t *testing.T) {
	db := acConnect(t)
	acCleanup(t, db)
	defer acCleanup(t, db)
	ctx := context.Background()
	projectID := acSeed(t, db)

	rumah := acSeedUnit(t, db, projectID, "R-01", "rumah", 100)
	acSeedUnit(t, db, projectID, "PDAM-01", "pdam", 250) // area besar: kalau ikut, dilusi besar

	repo := allocation.NewGORMRepository(db)
	inputs, err := repo.GetUnitInputs(ctx, acTenant, projectID)
	if err != nil {
		t.Fatalf("GetUnitInputs: %v", err)
	}
	if len(inputs) != 1 || inputs[0].UnitID != rumah {
		t.Fatalf("basis alokasi = %d unit (%v), want hanya unit properti (id=%d)", len(inputs), inputs, rumah)
	}
	if !inputs[0].SaleableArea.Equal(domain.FromInt(100).Decimal()) {
		t.Errorf("saleable_area unit properti = %s, want 100", inputs[0].SaleableArea)
	}
}

// Proyek yang HANYA berisi produk non-properti tidak punya penerima alokasi yang
// sah. Engine menolak, bukan diam-diam membuang biaya (Invariant #3).
func TestIntegration_GetUnitInputs_OnlyNonProperty_YieldsEmptyBasis(t *testing.T) {
	db := acConnect(t)
	acCleanup(t, db)
	defer acCleanup(t, db)
	ctx := context.Background()
	projectID := acSeed(t, db)
	acSeedUnit(t, db, projectID, "PDAM-01", "pdam", 250)

	inputs, err := repo(db).GetUnitInputs(ctx, acTenant, projectID)
	if err != nil {
		t.Fatalf("GetUnitInputs: %v", err)
	}
	if len(inputs) != 0 {
		t.Fatalf("basis = %d unit, want 0", len(inputs))
	}
	if _, err := allocation.Compute(domain.UnitCostBreakdown{}, inputs, allocation.BasisSaleableArea); !errors.Is(err, allocation.ErrNoUnits) {
		t.Errorf("Compute dengan basis kosong: want ErrNoUnits, got %v", err)
	}
}

// FAIL-CLOSED: unit_type di luar katalog tidak boleh ditebak (bukan properti
// diam-diam, bukan dibuang diam-diam) — perhitungan dibatalkan dengan error.
func TestIntegration_GetUnitInputs_UnregisteredType_FailsClosed(t *testing.T) {
	db := acConnect(t)
	acCleanup(t, db)
	defer acCleanup(t, db)
	ctx := context.Background()
	projectID := acSeed(t, db)
	acSeedUnit(t, db, projectID, "R-01", "rumah", 100)
	acSeedUnit(t, db, projectID, "G-01", "hotel", 100) // tidak ada di katalog

	if _, err := repo(db).GetUnitInputs(ctx, acTenant, projectID); !errors.Is(err, allocation.ErrUnitTypeUnregistered) {
		t.Fatalf("want ErrUnitTypeUnregistered, got %v", err)
	}
}

// Isolasi tenant (Invariant #6): katalog tenant lain tidak dipakai memutuskan
// kategori unit tenant ini — kalau bocor, unit di sini akan terlihat "terdaftar".
func TestIntegration_GetUnitInputs_CatalogTenantScoped(t *testing.T) {
	db := acConnect(t)
	acCleanup(t, db)
	defer acCleanup(t, db)
	ctx := context.Background()
	projectID := acSeed(t, db)
	acSeedUnit(t, db, projectID, "X-01", "kondotel", 100)

	// Katalog milik TENANT LAIN memuat 'kondotel'.
	const other = acTenant + 1
	db.Exec(`DELETE FROM product_types WHERE tenant_id = ?`, other)
	if err := db.Exec(`INSERT INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, other, "kondotel", "Kondotel", "property", "4-1000").Error; err != nil {
		t.Fatalf("seed katalog tenant lain: %v", err)
	}
	defer db.Exec(`DELETE FROM product_types WHERE tenant_id = ?`, other)

	if _, err := repo(db).GetUnitInputs(ctx, acTenant, projectID); !errors.Is(err, allocation.ErrUnitTypeUnregistered) {
		t.Fatalf("katalog bocor lintas tenant: got %v", err)
	}
}

func repo(db *gorm.DB) *allocation.GORMRepository { return allocation.NewGORMRepository(db) }
