//go:build integration

package closing_test

// P0-4 — integration (real MySQL): siklus penuh Completion & True-Up.
// Skenario design §10: 2 unit (A 100m² sold, B 300m² unsold), RAB land 400jt,
// aktual land 480jt. Land SELALU rata antar unit properti (rule klien UAT #1)
// — basis area/nilai jual proyek TIDAK berlaku untuk Land, jadi A dan B masing
// 50%. BAST A budgeted (200jt) → completion → finalize → calculate (A: bud
// 200, act 240, Δ+40; B: act 240 unsold) → approve → post (Dr 5-1000 40jt /
// Cr 1-3000 40jt) → TU-1..TU-5 → BAST B pakai finalized act_HPP 240jt
// (TU-6/D2) → saldo 1-3000 = 0 (TU-4). Plus D1 freeze basis.
//
// Prasyarat: TEST_DB_DSN + migrasi ≥ 000044.

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/closing"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

const ctTenant uint64 = 9_900_008

func ctConnect(t *testing.T) *gorm.DB {
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

func ctCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"hpp_trueup_lines", "hpp_trueup_runs", "project_completion_events",
		"allocation_snapshot_lines", "allocation_snapshots", "sale_records",
		"journal_lines", "journal_entries",
		"allocation_config_versions", "allocation_configs", "allocation_executions",
		"budget_items", "budget_plans", "termin_payments",
		"unit_status_transitions", "units", "product_types", "project_phases", "projects", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", ctTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func ctSeedAccounts(t *testing.T, db *gorm.DB) map[string]uint64 {
	t.Helper()
	accs := []ledger.Account{
		{TenantID: ctTenant, Code: "2-2000", Name: "Uang Muka Penjualan", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: ctTenant, Code: "1-2000", Name: "Piutang Usaha", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: ctTenant, Code: "4-1000", Name: "Pendapatan Penjualan Unit", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: ctTenant, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: ctTenant, Code: "2-1000", Name: "Hutang Usaha", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: ctTenant, Code: "1-3000", Name: "Persediaan Tanah", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: ctTenant, Code: "1-3100", Name: "Persediaan Hard Cost", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: ctTenant, Code: "1-3200", Name: "Persediaan Soft Cost", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: ctTenant, Code: "1-3300", Name: "Persediaan Financing", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
	}
	ids := map[string]uint64{}
	for i := range accs {
		if err := db.Create(&accs[i]).Error; err != nil {
			t.Fatalf("seed account %s: %v", accs[i].Code, err)
		}
		ids[accs[i].Code] = accs[i].ID
	}
	return ids
}

// ctBalance: saldo debit-normal akun (Σdebit − Σkredit) dari POSTED lines.
func ctBalance(t *testing.T, db *gorm.DB, code string) string {
	t.Helper()
	var s struct{ Bal string }
	if err := db.Raw(`
		SELECT COALESCE(SUM(jl.debit - jl.credit), 0) AS bal
		FROM journal_lines jl
		JOIN journal_entries je ON je.id = jl.journal_entry_id
		JOIN accounts a ON a.id = jl.account_id
		WHERE je.tenant_id = ? AND je.posted_at IS NOT NULL AND a.code = ?`, ctTenant, code).
		Scan(&s).Error; err != nil {
		t.Fatalf("saldo %s: %v", code, err)
	}
	return s.Bal
}

func TestIntegration_P04_FullTrueupCycle(t *testing.T) {
	db := ctConnect(t)
	ctCleanup(t, db)
	defer ctCleanup(t, db)
	ctx := context.Background()

	// ── Seed ──────────────────────────────────────────────────────────────────
	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		ctTenant, "P04 Project", "selling").Error; err != nil {
		t.Fatal(err)
	}
	var projectID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)
	seedUnit := func(code string, area int64) uint64 {
		// Product Catalog (fail-closed): unit_type WAJIB terdaftar di katalog.
		// Kategori 'property' + akun 4-1000 = perilaku fallback lama, jadi jurnal
		// yang dihasilkan fixture ini identik dengan sebelum hardening.
		db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
			VALUES (?,?,?,?,?,TRUE)`, ctTenant, "villa", "villa", "property", "4-1000")
		if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
			VALUES (?,?,?,?,?,?,?,?)`, ctTenant, projectID, code, "villa", domain.FromInt(area), domain.FromInt(area), domain.FromInt(0), "reserved").Error; err != nil {
			t.Fatalf("seed unit %s: %v", code, err)
		}
		var id uint64
		db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
		return id
	}
	unitA := seedUnit("A-01", 100) // area 100/400 = 25% (Item 9, proporsional land_area)
	unitB := seedUnit("B-01", 300) // area 300/400 = 75% (Item 9, proporsional land_area)
	accIDs := ctSeedAccounts(t, db)

	// ── Wiring produksi (mirror sale.NewHandler + closing) ────────────────────
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo, allocation.WithVersionStore(allocRepo))
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)
	budgetSvc := budget.NewService(budget.NewGORMRepository(db), budget.NewGORMRealisasiProvider(db))
	resolver := sale.NewBudgetedHPPResolver(budgetSvc, allocSvc, saleRepo)
	resolver.SetConfigVersionSource(allocSvc)
	closingSvc := closing.NewService(db)
	saleSvc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithHPPResolver(resolver))
	saleSvc.SetFinalizedHPPSource(closingSvc) // D2 (closing memenuhi kontrak sale)

	// ── RAB land 400jt (approve) + basis area ─────────────────────────────────
	plan, err := budgetSvc.CreatePlan(ctx, ctTenant, budget.CreatePlanRequest{ProjectID: projectID, Label: "RAB"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := budgetSvc.AddItem(ctx, ctTenant, budget.AddItemRequest{
		PlanID: plan.ID, Category: budget.BudgetCategoryLand, BudgetedAmount: domain.FromInt(400_000_000), Description: "land",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := budgetSvc.ApprovePlan(ctx, ctTenant, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "owner"}); err != nil {
		t.Fatal(err)
	}
	if err := allocSvc.SetBasis(ctx, ctTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatal(err)
	}

	// ── Biaya aktual land 480jt (posted, tag project) ────────────────────────
	pid := projectID
	entry, err := posting.Create(ctx, ledger.CreateJournalRequest{
		TenantID: ctTenant, Date: time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		Description: "Biaya tanah aktual",
		Lines: []ledger.LineInput{
			{AccountID: accIDs["1-3000"], Debit: domain.FromInt(480_000_000), ProjectID: &pid},
			{AccountID: accIDs["2-1000"], Credit: domain.FromInt(480_000_000), ProjectID: &pid},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := posting.Post(ctx, ctTenant, entry.ID); err != nil {
		t.Fatal(err)
	}

	// ── BAST A (budgeted, Land proporsional area 25% × 400jt = 100jt — Item 9) ─
	recA, err := saleSvc.RecordAkad(ctx, ctTenant, sale.RecordBASTRequest{
		UnitID: unitA, SalePrice: domain.FromInt(2_000_000_000),
		BASTDate: time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BAST A: %v", err)
	}
	if recA.HPPMethod != sale.HPPMethodBudgeted || recA.HPPLand.String() != "100000000" {
		t.Fatalf("BAST A: method=%s land=%s, want budgeted/100000000", recA.HPPMethod, recA.HPPLand)
	}
	// Snapshot pin version basis (D1, migration 000030 hidup).
	var pin struct {
		VersionID *uint64 `gorm:"column:allocation_config_version_id"`
		Version   *int    `gorm:"column:allocation_config_version"`
	}
	db.Raw(`SELECT allocation_config_version_id, allocation_config_version
		FROM allocation_snapshots WHERE tenant_id=? AND unit_id=?`, ctTenant, unitA).Scan(&pin)
	if pin.VersionID == nil || pin.Version == nil || *pin.Version != 1 {
		t.Fatalf("snapshot tidak mem-pin version basis: %+v", pin)
	}
	// Identitas historis di lines.
	var lineIdent struct {
		UnitID uint64
		Name   string `gorm:"column:unit_name_snapshot"`
	}
	db.Raw(`SELECT l.unit_id, l.unit_name_snapshot FROM allocation_snapshot_lines l
		JOIN allocation_snapshots s ON s.id = l.snapshot_id
		WHERE s.tenant_id=? AND s.unit_id=? LIMIT 1`, ctTenant, unitA).Scan(&lineIdent)
	if lineIdent.UnitID != unitA || lineIdent.Name != "A-01" {
		t.Fatalf("identitas snapshot line: %+v", lineIdent)
	}

	// ── Guard: calculate sebelum finalized → ditolak ─────────────────────────
	if _, err := closingSvc.Calculate(ctx, ctTenant, projectID, nil); !errors.Is(err, closing.ErrCompletionNotFinalized) {
		t.Fatalf("calculate pra-finalized: want ErrCompletionNotFinalized, got %v", err)
	}

	// ── Completion: mark + finalize ──────────────────────────────────────────
	if _, err := closingSvc.MarkCompleted(ctx, ctTenant, projectID, time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC), nil); err != nil {
		t.Fatalf("MarkCompleted: %v", err)
	}
	var pstatus string
	db.Raw("SELECT status FROM projects WHERE id=?", projectID).Scan(&pstatus)
	if pstatus != "completed" {
		t.Errorf("projects.status = %s, want completed", pstatus)
	}
	if _, err := closingSvc.FinalizeCompletion(ctx, ctTenant, projectID, nil); err != nil {
		t.Fatalf("Finalize: %v", err)
	}

	// ── D2 guard: finalized tapi true-up belum posted → BAST B diblokir ──────
	if _, err := saleSvc.RecordAkad(ctx, ctTenant, sale.RecordBASTRequest{
		UnitID: unitB, SalePrice: domain.FromInt(3_000_000_000),
		BASTDate: time.Date(2026, 7, 5, 0, 0, 0, 0, time.UTC),
	}); !errors.Is(err, closing.ErrTrueupNotPosted) {
		t.Fatalf("BAST B pra-trueup: want ErrTrueupNotPosted, got %v", err)
	}

	// ── Preview + Calculate ──────────────────────────────────────────────────
	prev, err := closingSvc.PreviewVariance(ctx, ctTenant, projectID)
	if err != nil {
		t.Fatalf("Preview: %v", err)
	}
	if prev.VarianceTotal.String() != "20000000" || prev.SoldUnits != 1 || prev.UnsoldUnits != 1 {
		t.Fatalf("preview: variance=%s sold=%d unsold=%d, want 20000000/1/1",
			prev.VarianceTotal, prev.SoldUnits, prev.UnsoldUnits)
	}
	run, err := closingSvc.Calculate(ctx, ctTenant, projectID, nil)
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if run.Status != closing.RunCalculated || run.VarianceTotal.String() != "20000000" ||
		run.BudgetHPPTotal.String() != "100000000" || run.ActualCostTotal.String() != "480000000" {
		t.Fatalf("run: %+v", run)
	}
	// Lines (Item 9, proporsional land_area): A land sold 100/120/+20 (25%);
	// B land unsold act 360 (75%).
	var gotA, gotB bool
	for _, l := range run.Lines {
		switch {
		case l.UnitID == unitA && l.Category == "land":
			gotA = true
			if !l.IsSold || l.BudgetedAmount.String() != "100000000" || l.ActualAmount.String() != "120000000" || l.VarianceAmount.String() != "20000000" {
				t.Errorf("line A: %+v", l)
			}
		case l.UnitID == unitB && l.Category == "land":
			gotB = true
			if l.IsSold || l.ActualAmount.String() != "360000000" || !l.VarianceAmount.IsZero() {
				t.Errorf("line B: %+v", l)
			}
		}
	}
	if !gotA || !gotB {
		t.Fatalf("lines tidak lengkap: A=%v B=%v (%d baris)", gotA, gotB, len(run.Lines))
	}

	// Guard: post sebelum approve → ditolak.
	if _, err := closingSvc.Post(ctx, ctTenant, run.ID, time.Now(), nil); !errors.Is(err, closing.ErrInvalidStateTransition) {
		t.Fatalf("post pra-approve: want ErrInvalidStateTransition, got %v", err)
	}

	// ── Approve + Post ───────────────────────────────────────────────────────
	if _, err := closingSvc.Approve(ctx, ctTenant, run.ID, nil); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	posted, err := closingSvc.Post(ctx, ctTenant, run.ID, time.Date(2026, 7, 10, 0, 0, 0, 0, time.UTC), nil)
	if err != nil {
		t.Fatalf("Post: %v", err)
	}
	if posted.Status != closing.RunPosted || posted.JournalID == nil {
		t.Fatalf("posted run: %+v", posted)
	}

	// TU-2: jurnal adjustment ada, 2 baris (Dr 5-1000 / Cr 1-3000), balanced by posting.
	var jn int64
	db.Raw("SELECT COUNT(*) FROM journal_lines WHERE journal_entry_id = ?", *posted.JournalID).Scan(&jn)
	if jn != 2 {
		t.Errorf("jurnal true-up: %d baris, want 2", jn)
	}
	// TU-1: saldo 1-3000 = 480 − 100 (BAST A) − 20 (true-up) = 360jt = act_HPP(B unsold).
	if bal := ctBalance(t, db, "1-3000"); bal != "360000000.0000" {
		t.Errorf("TU-1: saldo 1-3000 = %s, want 360000000.0000", bal)
	}
	// TU-3: Σ HPP sold (BAST 100 + true-up 20) == act_HPP(A) 120jt (saldo 5-1000).
	if bal := ctBalance(t, db, "5-1000"); bal != "120000000.0000" {
		t.Errorf("TU-3: saldo 5-1000 = %s, want 120000000.0000", bal)
	}

	// TU-5: double-post → ditolak, jurnal tidak bertambah.
	if _, err := closingSvc.Post(ctx, ctTenant, run.ID, time.Now(), nil); !errors.Is(err, closing.ErrInvalidStateTransition) {
		t.Fatalf("double-post: want ErrInvalidStateTransition, got %v", err)
	}
	var totalTrueupJournals int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id=? AND source='hpp_trueup'", ctTenant).Scan(&totalTrueupJournals)
	if totalTrueupJournals != 1 {
		t.Errorf("TU-5: %d jurnal true-up, want 1", totalTrueupJournals)
	}

	// ── TU-6 / D2: BAST B pasca-posting memakai act_HPP FINALIZED (360jt) ────
	recB, err := saleSvc.RecordAkad(ctx, ctTenant, sale.RecordBASTRequest{
		UnitID: unitB, SalePrice: domain.FromInt(3_000_000_000),
		BASTDate: time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("BAST B (finalized): %v", err)
	}
	if recB.HPPMethod != sale.HPPMethodFinalized || recB.HPPLand.String() != "360000000" {
		t.Fatalf("BAST B: method=%s land=%s, want finalized/360000000", recB.HPPMethod, recB.HPPLand)
	}
	// TU-4: semua unit terjual ⇒ saldo 1-3000 = 0.
	if bal := ctBalance(t, db, "1-3000"); bal != "0.0000" {
		t.Errorf("TU-4: saldo 1-3000 = %s, want 0.0000", bal)
	}

	// ── D1: ganti basis SETELAH snapshot ada → version baru (audit) ──────────
	if err := allocSvc.SetBasis(ctx, ctTenant, projectID, allocation.BasisSalesValue); err != nil {
		t.Fatalf("SetBasis pasca-freeze: %v", err)
	}
	var vcount int64
	db.Raw("SELECT COUNT(*) FROM allocation_config_versions WHERE tenant_id=? AND project_id=?", ctTenant, projectID).Scan(&vcount)
	if vcount != 2 {
		t.Errorf("D1: %d version rows, want 2 (v1 superseded + v2 aktif)", vcount)
	}
	var activeVer int
	db.Raw("SELECT version FROM allocation_config_versions WHERE tenant_id=? AND project_id=? AND active_key='Y'", ctTenant, projectID).Scan(&activeVer)
	if activeVer != 2 {
		t.Errorf("D1: version aktif = %d, want 2", activeVer)
	}
}
