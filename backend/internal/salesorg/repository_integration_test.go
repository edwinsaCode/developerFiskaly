//go:build integration

package salesorg_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/salesorg"
)

const soTenant uint64 = 9_900_011

func soConnect(t *testing.T) *gorm.DB {
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

func soCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"sales_persons", "sales_teams"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", soTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func TestIntegration_SalesOrg_RoundTrip(t *testing.T) {
	db := soConnect(t)
	soCleanup(t, db)
	defer soCleanup(t, db)
	ctx := context.Background()
	svc := salesorg.NewService(salesorg.NewGORMRepository(db), salesorg.NewGORMRepository(db))

	team, err := svc.CreateTeam(ctx, soTenant, salesorg.CreateTeamRequest{Code: "TEAM-INT", Name: "Tim Integrasi"})
	if err != nil {
		t.Fatalf("CreateTeam: %v", err)
	}

	// UNIQUE code team.
	if _, err := svc.CreateTeam(ctx, soTenant, salesorg.CreateTeamRequest{Code: "TEAM-INT", Name: "Dup"}); !errors.Is(err, salesorg.ErrTeamCodeDup) {
		t.Errorf("team code dup harus ErrTeamCodeDup, got %v", err)
	}

	// Person tertaut team valid.
	p, err := svc.CreatePerson(ctx, soTenant, salesorg.CreatePersonRequest{Code: "SP-INT", Name: "Sari", SalesTeamID: &team.ID})
	if err != nil {
		t.Fatalf("CreatePerson: %v", err)
	}
	if p.SalesTeamID == nil || *p.SalesTeamID != team.ID {
		t.Errorf("person harus tertaut team %d", team.ID)
	}

	// Isolasi tenant.
	if _, err := svc.GetPerson(ctx, soTenant+1, p.ID); !errors.Is(err, salesorg.ErrPersonNotFound) {
		t.Errorf("lintas tenant harus ErrPersonNotFound, got %v", err)
	}

	// Set leader = person, lalu persist.
	if _, err := svc.UpdateTeam(ctx, soTenant, team.ID, salesorg.UpdateTeamRequest{LeaderSalesPersonID: &p.ID}); err != nil {
		t.Fatalf("UpdateTeam leader: %v", err)
	}
	got, _ := svc.GetTeam(ctx, soTenant, team.ID)
	if got.LeaderSalesPersonID == nil || *got.LeaderSalesPersonID != p.ID {
		t.Errorf("leader tak persist: %+v", got)
	}
}
