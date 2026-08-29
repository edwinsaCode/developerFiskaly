//go:build integration

package project_test

import (
	"context"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/project"
)

// TestSeedLITHOSIdempotent verifies that running SeedLITHOS twice produces
// identical row counts (no duplicate projects, phases, or units).
//
// Run with a real MySQL connection:
//
//	TEST_DB_DSN="user:pass@tcp(localhost:3307)/esaproperti?parseTime=true" \
//	  go test -tags integration -run TestSeedLITHOSIdempotent ./internal/project/...
func TestSeedLITHOSIdempotent(t *testing.T) {
	dsn := os.Getenv("TEST_DB_DSN")
	if dsn == "" {
		t.Skip("TEST_DB_DSN not set — skipping integration test")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		Logger: logger.Discard,
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	const tenantID uint64 = 99 // use a dedicated test tenant to avoid polluting real data

	// Clean up before and after the test
	cleanup := func() {
		db.Exec("DELETE FROM units WHERE tenant_id = ?", tenantID)
		db.Exec("DELETE FROM project_phases WHERE tenant_id = ?", tenantID)
		db.Exec("DELETE FROM projects WHERE tenant_id = ?", tenantID)
	}
	cleanup()
	t.Cleanup(cleanup)

	ctx := context.Background()

	// First run — should create everything
	if err := project.SeedLITHOS(ctx, db, tenantID); err != nil {
		t.Fatalf("first SeedLITHOS: %v", err)
	}

	count := func(table string) int64 {
		var n int64
		db.Table(table).Where("tenant_id = ?", tenantID).Count(&n)
		return n
	}

	projects1 := count("projects")
	phases1 := count("project_phases")
	units1 := count("units")

	if projects1 != 1 {
		t.Errorf("after first run: want 1 project, got %d", projects1)
	}
	if phases1 != 4 {
		t.Errorf("after first run: want 4 phases, got %d", phases1)
	}
	if units1 != 18 {
		t.Errorf("after first run: want 18 units, got %d", units1)
	}

	// Second run — should skip everything, counts must be identical
	if err := project.SeedLITHOS(ctx, db, tenantID); err != nil {
		t.Fatalf("second SeedLITHOS: %v", err)
	}

	projects2 := count("projects")
	phases2 := count("project_phases")
	units2 := count("units")

	if projects2 != projects1 {
		t.Errorf("idempotency: projects %d → %d (should stay %d)", projects1, projects2, projects1)
	}
	if phases2 != phases1 {
		t.Errorf("idempotency: phases %d → %d (should stay %d)", phases1, phases2, phases1)
	}
	if units2 != units1 {
		t.Errorf("idempotency: units %d → %d (should stay %d)", units1, units2, units1)
	}
}
