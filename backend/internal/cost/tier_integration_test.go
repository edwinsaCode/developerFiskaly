//go:build integration

package cost_test

// Increment 2 — integration test Cost 3-tier terhadap MySQL nyata.
// Membuktikan di level LEDGER (bukan mock):
//   1. Overhead posting ke akun beban 5-3000/5-4000 — pool Persediaan/HPP
//      (AccumulatedByProject, 1-3xxx) TIDAK tercemar.
//   2. Overhead Tenant-level (tanpa project) sah; journal lines tanpa tag project.
//   3. Jalur kapitalisasi (shared/direct) tetap mengisi 1-3xxx seperti sebelumnya.
// Prasyarat: TEST_DB_DSN di-set + DB termigrasi (s/d 000035).

import (
	"context"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/document"
	"esaproperti/internal/cost"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
)

const ctTenant uint64 = 9_900_003

func ctConnect(t *testing.T) *gorm.DB {
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

func ctCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{"cost_entries", "documents", "document_sequences", "journal_lines", "journal_entries", "accounts", "projects"} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", ctTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
	// Master jenis dokumen — produksi menanamnya saat tenant dibuat; resolver
	// fail-closed, jadi jalur kas tidak bisa menerbitkan dokumen tanpa ini.
	if err := document.SeedDefaultDocumentTypes(context.Background(), db, ctTenant); err != nil {
		t.Fatalf("seed jenis dokumen: %v", err)
	}
}

func ctService(t *testing.T, db *gorm.DB) *cost.Service {
	t.Helper()
	if err := ledger.SeedCOA(context.Background(), db, ctTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	repo := cost.NewGORMRepository(db)
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo)
	adapter := cost.NewLedgerJournalAdapter(posting)
	return cost.NewService(repo, adapter, repo, repo, nil)
}

func ctSeedProject(t *testing.T, db *gorm.DB, id uint64) {
	t.Helper()
	if err := db.Exec(`INSERT INTO projects (id, tenant_id, name, status, created_at, updated_at)
		VALUES (?,?,?,?,NOW(3),NOW(3))`, id, ctTenant, "IT Tier Project", "planning").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
}

// ctDebitAccountCode mengembalikan kode akun sisi debit dari jurnal sebuah entry.
func ctDebitAccountCode(t *testing.T, db *gorm.DB, journalID uint64) string {
	t.Helper()
	var code string
	err := db.Raw(`SELECT a.code FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		WHERE jl.journal_entry_id = ? AND jl.debit > 0`, journalID).Scan(&code).Error
	if err != nil {
		t.Fatalf("query debit account: %v", err)
	}
	return code
}

func ctBaseReq(projectID uint64) cost.CreateCostEntryRequest {
	return cost.CreateCostEntryRequest{
		ProjectID:       projectID,
		Category:        domain.CostCategoryMarketing,
		CostTier:        domain.CostTierOverhead,
		Amount:          domain.FromInt(30_000_000),
		PaymentMethod:   cost.PaymentMethodBank,
		BankAccountCode: "1-1300",
		Date:            time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
		Vendor:          "Agensi Iklan",
		Description:     "Kampanye pemasaran",
	}
}

func TestIntegration_Tier_Overhead_ExpenseAccount_HPPPoolClean(t *testing.T) {
	db := ctConnect(t)
	ctCleanup(t, db)
	defer ctCleanup(t, db)

	const project = uint64(777)
	ctSeedProject(t, db, project)
	svc := ctService(t, db)
	ctx := context.Background()

	// Overhead marketing (dengan project sebagai cost center) → post.
	entry, err := svc.CreateCostEntry(ctx, ctTenant, ctBaseReq(project))
	if err != nil {
		t.Fatalf("create overhead: %v", err)
	}
	if err := svc.PostCostEntry(ctx, ctTenant, entry.ID); err != nil {
		t.Fatalf("post overhead: %v", err)
	}
	if entry.CostTier != domain.CostTierOverhead {
		t.Errorf("CostTier = %q, want overhead", entry.CostTier)
	}
	if code := ctDebitAccountCode(t, db, entry.JournalEntryID); code != "5-3000" {
		t.Errorf("debit account = %s, want 5-3000 (Beban Pemasaran)", code)
	}

	// Pool HPP (1-3xxx) HARUS tetap kosong — overhead tidak mencemari alokasi/HPP.
	b, err := svc.AccumulatedByProject(ctx, ctTenant, project)
	if err != nil {
		t.Fatalf("accumulated: %v", err)
	}
	if !b.Total().IsZero() {
		t.Errorf("pool Persediaan harus 0 setelah overhead posting, got %s", b.Total())
	}

	// Kontrol: shared hard cost → masuk 1-3100 dan pool terisi.
	shared := ctBaseReq(project)
	shared.Category = domain.CostCategoryHard
	shared.CostTier = domain.CostTierShared
	shared.HardSubcategory = domain.ConstructionSaranaPrasarana // UAT 2026-09-07: wajib untuk tier=shared
	shared.Amount = domain.FromInt(100_000_000)
	shared.Description = "Jalan cluster"
	se, err := svc.CreateCostEntry(ctx, ctTenant, shared)
	if err != nil {
		t.Fatalf("create shared: %v", err)
	}
	if err := svc.PostCostEntry(ctx, ctTenant, se.ID); err != nil {
		t.Fatalf("post shared: %v", err)
	}
	if code := ctDebitAccountCode(t, db, se.JournalEntryID); code != "1-3100" {
		t.Errorf("debit account shared = %s, want 1-3100", code)
	}
	b, _ = svc.AccumulatedByProject(ctx, ctTenant, project)
	if b.Hard.String() != "100000000" {
		t.Errorf("pool hard = %s, want 100000000 (hanya biaya kapitalisasi)", b.Hard.String())
	}
	if b.Total().String() != "100000000" {
		t.Errorf("pool total = %s, want 100000000 (overhead 30jt TIDAK ikut)", b.Total().String())
	}
}

func TestIntegration_Tier_Overhead_TenantLevel_NoProject(t *testing.T) {
	db := ctConnect(t)
	ctCleanup(t, db)
	defer ctCleanup(t, db)

	svc := ctService(t, db)
	ctx := context.Background()

	req := ctBaseReq(0) // tanpa project — biaya perusahaan
	req.Category = domain.CostCategoryOther
	req.Description = "Sewa kantor pusat"
	entry, err := svc.CreateCostEntry(ctx, ctTenant, req)
	if err != nil {
		t.Fatalf("create overhead tenant-level: %v", err)
	}
	if err := svc.PostCostEntry(ctx, ctTenant, entry.ID); err != nil {
		t.Fatalf("post: %v", err)
	}
	if entry.ProjectID != nil {
		t.Errorf("ProjectID harus NULL, got %v", *entry.ProjectID)
	}
	if code := ctDebitAccountCode(t, db, entry.JournalEntryID); code != "5-4000" {
		t.Errorf("debit account = %s, want 5-4000 (Beban Umum & Administrasi)", code)
	}

	// Journal lines tanpa tag project.
	var n int64
	db.Raw(`SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = ? AND project_id IS NOT NULL`,
		entry.JournalEntryID).Scan(&n)
	if n != 0 {
		t.Errorf("journal lines tidak boleh ber-tag project untuk overhead Tenant-level, got %d baris", n)
	}

	// Round-trip store: cost_tier & project NULL persist di MySQL.
	stored, err := svc.GetCostEntry(ctx, ctTenant, entry.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if stored.CostTier != domain.CostTierOverhead || stored.ProjectID != nil {
		t.Errorf("stored: tier=%q project=%v, want overhead/NULL", stored.CostTier, stored.ProjectID)
	}
}
