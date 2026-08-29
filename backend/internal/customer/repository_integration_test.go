//go:build integration

package customer_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/customer"
)

// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (≥ 000033).
const cTenant uint64 = 9_900_010

func cConnect(t *testing.T) *gorm.DB {
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

func cCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec("DELETE FROM customers WHERE tenant_id = ?", cTenant).Error; err != nil {
		t.Fatalf("cleanup: %v", err)
	}
}

func TestIntegration_Customer_RoundTrip(t *testing.T) {
	db := cConnect(t)
	cCleanup(t, db)
	defer cCleanup(t, db)
	ctx := context.Background()
	repo := customer.NewGORMRepository(db)

	c := &customer.Customer{
		TenantID: cTenant, Code: "CUST-INT-1", Name: "PT Sejahtera",
		Type: customer.CustomerTypeCompany, NPWP: "01.234.567.8-999.000", IsActive: true,
	}
	if err := repo.Create(ctx, c); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if c.ID == 0 {
		t.Fatal("ID harus terisi setelah create")
	}

	// Baca kembali (tenant benar).
	got, err := repo.FindByID(ctx, cTenant, c.ID)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if got.Name != "PT Sejahtera" || got.Type != customer.CustomerTypeCompany {
		t.Errorf("data tidak konsisten: %+v", got)
	}

	// Isolasi tenant: tenant lain tak menemukan.
	if _, err := repo.FindByID(ctx, cTenant+1, c.ID); !errors.Is(err, customer.ErrCustomerNotFound) {
		t.Errorf("lintas tenant harus ErrCustomerNotFound, got %v", err)
	}

	// UNIQUE (tenant, code) ditegakkan DB → ErrCustomerCodeDuplicate.
	dup := &customer.Customer{TenantID: cTenant, Code: "CUST-INT-1", Name: "Lain", Type: customer.CustomerTypeIndividual, IsActive: true}
	if err := repo.Create(ctx, dup); !errors.Is(err, customer.ErrCustomerCodeDuplicate) {
		t.Errorf("kode duplikat harus ErrCustomerCodeDuplicate, got %v", err)
	}

	// Update persist.
	got.Name = "PT Sejahtera Abadi"
	got.IsActive = false
	if err := repo.Update(ctx, got); err != nil {
		t.Fatalf("Update: %v", err)
	}
	re, _ := repo.FindByID(ctx, cTenant, c.ID)
	if re.Name != "PT Sejahtera Abadi" || re.IsActive {
		t.Errorf("update tak persist: %+v", re)
	}
}
