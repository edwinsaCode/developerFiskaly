//go:build integration

package budget_test

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

// P1-1 + S5: bukti Decision A (posted-only) — realisasi HANYA menghitung baris
// LEDGER yang jurnalnya sudah DIPOSTING; draft (posted_at NULL) TIDAK dihitung.
// S5 (registry #13): realisasi dibaca dari journal_lines via pembaca kanonik
// ledger.ActualCostByCode — bukan lagi Σ cost_entries.amount — sehingga test
// ini menyemai jurnal + baris jurnal produksi (COA via ledger.SeedCOA).
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (sama seperti sale integration test).

const bTenant uint64 = 9_900_002

func bConnect(t *testing.T) *gorm.DB {
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

func bCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"cost_entries", "journal_lines", "journal_entries", "accounts", "projects"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", bTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func bSeedProject(t *testing.T, db *gorm.DB, id uint64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO projects (id, tenant_id, name, status, created_at, updated_at)
		VALUES (?,?,?,?,NOW(3),NOW(3))`, id, bTenant, "IT Project", "planning").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

// bSeedJournal membuat journal_entries; posted=true → posted_at diisi.
func bSeedJournal(t *testing.T, db *gorm.DB, posted bool) uint64 {
	t.Helper()
	var postedAt interface{}
	if posted {
		postedAt = time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	}
	res := db.Exec(`INSERT INTO journal_entries (tenant_id, date, description, posted_at, source, is_reversing, created_at, updated_at)
		VALUES (?,?,?,?,?,0,NOW(3),NOW(3))`,
		bTenant, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), "test", postedAt, "system")
	if res.Error != nil {
		t.Fatalf("seed journal: %v", res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func bSeedCost(t *testing.T, db *gorm.DB, projectID, journalID uint64, cat string, amount int64) {
	t.Helper()
	// cost_tier NOT NULL sejak 000035: kategori beban → overhead, lainnya → shared
	// (seed tanpa unit_id = pool proyek), konsisten dengan aturan backfill.
	tier := "shared"
	if cat == "marketing" || cat == "other" {
		tier = "overhead"
	}
	if err := db.Exec(`INSERT INTO cost_entries (tenant_id, project_id, category, cost_tier, amount, payment_method, bank_account_code, date, vendor, description, journal_entry_id, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,NOW(3),NOW(3))`,
		bTenant, projectID, cat, tier, domain.FromInt(amount), "bank", "1-1300", "2026-07-01", "V", "", journalID).Error; err != nil {
		t.Fatalf("seed cost: %v", err)
	}
	// S5: realisasi dibaca dari LEDGER — semai baris jurnal seperti posting
	// produksi cost.Service: Dr akun kategori (project-tagged) / Cr 2-1000.
	cc := domain.CostCategory(cat)
	debitCode := cc.InventoryAccountCode()
	if cc.IsExpense() {
		debitCode = cc.ExpenseAccountCode()
	}
	for _, ln := range []struct {
		code  string
		debit int64
	}{{debitCode, amount}, {"2-1000", 0}} {
		var accID uint64
		db.Raw(`SELECT id FROM accounts WHERE tenant_id = ? AND code = ?`, bTenant, ln.code).Scan(&accID)
		if accID == 0 {
			t.Fatalf("akun %s tidak ada (SeedCOA belum jalan?)", ln.code)
		}
		debit, credit := domain.FromInt(ln.debit), domain.Zero
		if ln.debit == 0 {
			debit, credit = domain.Zero, domain.FromInt(amount)
		}
		if err := db.Exec(`INSERT INTO journal_lines (tenant_id, journal_entry_id, account_id, debit, credit, project_id, created_at, updated_at)
			VALUES (?,?,?,?,?,?,NOW(3),NOW(3))`,
			bTenant, journalID, accID, debit, credit, projectID).Error; err != nil {
			t.Fatalf("seed journal line %s: %v", ln.code, err)
		}
	}
}

func TestIntegration_Realisasi_PostedOnly(t *testing.T) {
	db := bConnect(t)
	bCleanup(t, db)
	defer bCleanup(t, db)

	const project = uint64(555)
	bSeedProject(t, db, project)
	if err := ledger.SeedCOA(context.Background(), db, bTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	postedJ := bSeedJournal(t, db, true)
	draftJ := bSeedJournal(t, db, false)
	bSeedCost(t, db, project, postedJ, "hard", 100_000_000) // POSTED → dihitung
	bSeedCost(t, db, project, draftJ, "hard", 50_000_000)   // DRAFT → TIDAK dihitung
	bSeedCost(t, db, project, postedJ, "land", 20_000_000)  // POSTED land

	prov := budget.NewGORMRealisasiProvider(db)
	got, err := prov.GetRealisasiByProject(context.Background(), bTenant, project, nil)
	if err != nil {
		t.Fatalf("GetRealisasiByProject: %v", err)
	}
	if v := got[domain.CostCategoryHard]; v.String() != "100000000" {
		t.Errorf("hard realisasi (posted-only): got %s, want 100000000 (draft 50jt dikecualikan)", v.String())
	}
	if v := got[domain.CostCategoryLand]; v.String() != "20000000" {
		t.Errorf("land realisasi: got %s, want 20000000", v.String())
	}
}

// Increment 2: realisasi marketing/other mengalir dari cost entry tier overhead
// (posted-only tetap berlaku — draft marketing tidak dihitung).
func TestIntegration_Realisasi_MarketingOther_ViaOverhead(t *testing.T) {
	db := bConnect(t)
	bCleanup(t, db)
	defer bCleanup(t, db)

	const project = uint64(556)
	bSeedProject(t, db, project)
	if err := ledger.SeedCOA(context.Background(), db, bTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	postedJ := bSeedJournal(t, db, true)
	draftJ := bSeedJournal(t, db, false)
	bSeedCost(t, db, project, postedJ, "marketing", 30_000_000) // POSTED → dihitung
	bSeedCost(t, db, project, draftJ, "marketing", 10_000_000)  // DRAFT → TIDAK
	bSeedCost(t, db, project, postedJ, "other", 5_000_000)      // POSTED other

	prov := budget.NewGORMRealisasiProvider(db)
	got, err := prov.GetRealisasiByProject(context.Background(), bTenant, project, nil)
	if err != nil {
		t.Fatalf("GetRealisasiByProject: %v", err)
	}
	if v := got[domain.CostCategoryMarketing]; v.String() != "30000000" {
		t.Errorf("marketing realisasi: got %s, want 30000000 (draft dikecualikan)", v.String())
	}
	if v := got[domain.CostCategoryOther]; v.String() != "5000000" {
		t.Errorf("other realisasi: got %s, want 5000000", v.String())
	}
}
