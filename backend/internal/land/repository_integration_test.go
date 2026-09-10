//go:build integration

package land_test

// LT-3 — integration test land.GORMRepository terhadap MySQL nyata.
// Membuktikan: FK ke projects ditegakkan, UNIQUE(tenant_id, project_id)
// ditegakkan, CHECK constraint capacity (INV-LAND-1) ditegakkan oleh DB
// (bukan cuma di service layer), dan isolasi tenant.
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (s/d 000079).

import (
	"context"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/customer"
	"esaproperti/internal/domain"
	"esaproperti/internal/land"
	"esaproperti/internal/project"
)

const (
	ltTenant      uint64 = 9_900_079
	ltOtherTenant uint64 = 9_900_080
)

func ltConnect(t *testing.T) *gorm.DB {
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

func ltCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"land_stock_reservations", "land_stock", "customers", "projects"} {
		for _, tenantID := range []uint64{ltTenant, ltOtherTenant} {
			if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", tenantID).Error; err != nil {
				t.Fatalf("cleanup %s: %v", tbl, err)
			}
		}
	}
}

func ltMustProject(t *testing.T, db *gorm.DB, tenantID uint64, name string) uint64 {
	t.Helper()
	p := project.Project{TenantID: tenantID, Name: name}
	if err := db.WithContext(context.Background()).Create(&p).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	return p.ID
}

func ltMustCustomer(t *testing.T, db *gorm.DB, tenantID uint64, code string) uint64 {
	t.Helper()
	c := customer.Customer{TenantID: tenantID, Code: code, Name: "LT Test Customer " + code}
	if err := db.WithContext(context.Background()).Create(&c).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	return c.ID
}

func ltMustPool(t *testing.T, repo *land.GORMRepository, tenantID, projectID uint64, total string) *land.LandStock {
	t.Helper()
	pool := &land.LandStock{
		TenantID: tenantID, ProjectID: projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.RequireFromString(total), UnitPrice: domain.FromInt(1_000_000),
	}
	if err := repo.CreatePool(context.Background(), pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	return pool
}

func TestIntegration_CreatePool_ForeignKeyEnforced(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	err := repo.CreatePool(context.Background(), &land.LandStock{
		TenantID: ltTenant, ProjectID: 999_999_999, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.NewFromInt(100),
	})
	if err != land.ErrProjectNotFound {
		t.Fatalf("want ErrProjectNotFound, got %v", err)
	}
}

func TestIntegration_CreatePool_UniquePerProjectEnforced(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Integration Project")

	if err := repo.CreatePool(context.Background(), &land.LandStock{
		TenantID: ltTenant, ProjectID: projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.NewFromInt(100),
	}); err != nil {
		t.Fatalf("first CreatePool: %v", err)
	}
	err := repo.CreatePool(context.Background(), &land.LandStock{
		TenantID: ltTenant, ProjectID: projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.NewFromInt(50),
	})
	if err != land.ErrLandStockAlreadyExists {
		t.Fatalf("want ErrLandStockAlreadyExists, got %v", err)
	}
}

func TestIntegration_UpdatePool_CapacityCheckEnforcedByDB(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Capacity Project")

	pool := &land.LandStock{
		TenantID: ltTenant, ProjectID: projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.NewFromInt(500),
	}
	if err := repo.CreatePool(context.Background(), pool); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	// Simulasikan reserved+sold sudah terkomit langsung lewat SQL (LT-4/LT-5
	// belum ada) — DB harus tetap menegakkan CHECK saat total diturunkan
	// di bawah yang sudah terkomit, sebagai backstop fail-closed independen
	// dari service layer.
	if err := db.Exec("UPDATE land_stock SET reserved_quantity_m2 = 200, sold_quantity_m2 = 250 WHERE id = ?", pool.ID).Error; err != nil {
		t.Fatalf("seed reserved/sold: %v", err)
	}

	err := repo.UpdatePoolQuantityAndPrice(context.Background(), ltTenant, pool.ID, decimal.NewFromInt(400), domain.FromInt(1000), domain.FromInt(800))
	if err != land.ErrCapacityExceeded {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}
}

func TestIntegration_TenantIsolation(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Isolation Project")

	if err := repo.CreatePool(context.Background(), &land.LandStock{
		TenantID: ltTenant, ProjectID: projectID, ProductCode: "kelebihan_tanah",
		TotalQuantityM2: decimal.NewFromInt(100),
	}); err != nil {
		t.Fatalf("CreatePool: %v", err)
	}

	_, err := repo.FindPoolByProject(context.Background(), ltOtherTenant, projectID)
	if err != land.ErrLandStockNotFound {
		t.Fatalf("cross-tenant read want ErrLandStockNotFound, got %v", err)
	}
}

// ── Reservasi (LT-4) ────────────────────────────────────────────────────────

func TestIntegration_Reserve_Success(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Reserve Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-RES-1")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")

	res, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.NewFromInt(200), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if res.Status != land.ReservationStatusActive {
		t.Errorf("status want active, got %s", res.Status)
	}

	got, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !got.ReservedQuantityM2.Equal(decimal.NewFromInt(200)) {
		t.Errorf("reserved_quantity_m2 want 200, got %s", got.ReservedQuantityM2)
	}
}

func TestIntegration_Reserve_CustomerFKEnforced(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Reserve FK Project")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")

	_, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: 999_999_999, QuantityM2: decimal.NewFromInt(100), ReservedAt: time.Now(),
	})
	if err != land.ErrCustomerNotFound {
		t.Fatalf("want ErrCustomerNotFound, got %v", err)
	}
	// Reservasi gagal harus rollback penuh — reserved_quantity_m2 tidak boleh berubah.
	got, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !got.ReservedQuantityM2.IsZero() {
		t.Errorf("reserved_quantity_m2 want 0 after rollback, got %s", got.ReservedQuantityM2)
	}
}

func TestIntegration_Reserve_CapacityExceededRejected(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Reserve Capacity Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-RES-2")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")

	_, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.NewFromInt(600), ReservedAt: time.Now(),
	})
	if err != land.ErrCapacityExceeded {
		t.Fatalf("want ErrCapacityExceeded, got %v", err)
	}
}

// TestIntegration_Reserve_ConcurrentRace: dua reservasi paralel terhadap sisa
// kuantitas yang sama (§K test matrix) — total gabungan (300+300=600) melebihi
// total pool (500). Row-lock SELECT...FOR UPDATE di Reserve wajib menyerialkan
// keduanya sehingga TEPAT SATU berhasil, bukan nol atau dua-duanya.
func TestIntegration_Reserve_ConcurrentRace(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Reserve Race Project")
	custA := ltMustCustomer(t, db, ltTenant, "LT-RACE-A")
	custB := ltMustCustomer(t, db, ltTenant, "LT-RACE-B")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")

	var wg sync.WaitGroup
	var successes int32
	run := func(custID uint64) {
		defer wg.Done()
		_, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
			ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.NewFromInt(300), ReservedAt: time.Now(),
		})
		if err == nil {
			atomic.AddInt32(&successes, 1)
		} else if err != land.ErrCapacityExceeded {
			t.Errorf("unexpected error: %v", err)
		}
	}

	wg.Add(2)
	go run(custA)
	go run(custB)
	wg.Wait()

	if successes != 1 {
		t.Fatalf("want exactly 1 successful reservation out of 2 racing, got %d", successes)
	}

	got, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !got.ReservedQuantityM2.Equal(decimal.NewFromInt(300)) {
		t.Errorf("reserved_quantity_m2 want 300 (only the winner), got %s", got.ReservedQuantityM2)
	}
}

func TestIntegration_CloseReservation_CancelReturnsQuantity(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Cancel Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-CANCEL-1")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")

	res, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.NewFromInt(200), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	closed, err := repo.CloseReservation(context.Background(), ltTenant, res.ID, land.ReservationStatusCancelled, "buyer batal")
	if err != nil {
		t.Fatalf("CloseReservation: %v", err)
	}
	if closed.Status != land.ReservationStatusCancelled {
		t.Errorf("status want cancelled, got %s", closed.Status)
	}

	got, err := repo.FindPoolByProject(context.Background(), ltTenant, projectID)
	if err != nil {
		t.Fatalf("FindPoolByProject: %v", err)
	}
	if !got.ReservedQuantityM2.IsZero() {
		t.Errorf("reserved_quantity_m2 want 0 (restored), got %s", got.ReservedQuantityM2)
	}

	// Menutup reservasi yang sudah terminal harus ditolak.
	_, err = repo.CloseReservation(context.Background(), ltTenant, res.ID, land.ReservationStatusCancelled, "")
	if err != land.ErrReservationNotActive {
		t.Fatalf("want ErrReservationNotActive, got %v", err)
	}
}

func TestIntegration_Reservation_TenantIsolation(t *testing.T) {
	db := ltConnect(t)
	ltCleanup(t, db)
	defer ltCleanup(t, db)

	repo := land.NewGORMRepository(db)
	projectID := ltMustProject(t, db, ltTenant, "LT Reservation Isolation Project")
	custID := ltMustCustomer(t, db, ltTenant, "LT-ISO-1")
	pool := ltMustPool(t, repo, ltTenant, projectID, "500")

	res, err := repo.Reserve(context.Background(), ltTenant, pool.ID, land.ReserveInput{
		ProjectID: projectID, CustomerID: custID, QuantityM2: decimal.NewFromInt(100), ReservedAt: time.Now(),
	})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}

	_, err = repo.FindReservation(context.Background(), ltOtherTenant, res.ID)
	if err != land.ErrReservationNotFound {
		t.Fatalf("cross-tenant read want ErrReservationNotFound, got %v", err)
	}
}
