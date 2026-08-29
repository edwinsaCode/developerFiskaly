//go:build integration

package sale_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/sale"
)

// P0-2/P0-3 — integration: BAST metode Budgeted Cost Allocation (real MySQL).
// Membuktikan: snapshot dipersist atomik, jurnal HPP balanced (Dr 5-1000 / Cr
// 1-3xxx), sale_record ter-link ke snapshot + versi RAB, BCA-1 Σ per class ==
// pool, snapshot immutable saat RAB di-supersede, dan fallback actual (legacy).
// Prasyarat sama: TEST_DB_DSN di-set + DB termigrasi (≥ 000027).

const bbTenant uint64 = 9_900_004

func bbCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"allocation_snapshot_lines", "allocation_snapshots", "sale_records",
		"journal_lines", "journal_entries", "allocation_executions",
		"allocation_configs", "budget_items", "budget_plans",
		"termin_payments", "unit_status_transitions", "units", "product_types", "project_phases", "projects", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", bbTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

func bbSeedProject(t *testing.T, db *gorm.DB) uint64 {
	t.Helper()
	res := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		bbTenant, "BCA Project", "selling")
	if res.Error != nil {
		t.Fatalf("seed project: %v", res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func bbSeedUnit(t *testing.T, db *gorm.DB, projectID uint64, code string, area int64) uint64 {
	t.Helper()
	// Status 'reserved': sejak Increment 6.1 (F-1) BAST hanya sah dari
	// reserved|ppjb — fixture mengikuti alur legal.
	// Product Catalog (fail-closed): unit_type WAJIB terdaftar di katalog.
	// Kategori 'property' + akun 4-1000 = perilaku fallback lama, jadi jurnal
	// yang dihasilkan fixture ini identik dengan sebelum hardening.
	db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, bbTenant, "villa", "villa", "property", "4-1000")
	res := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
		VALUES (?,?,?,?,?,?,?)`,
		bbTenant, projectID, code, "villa", domain.FromInt(area), domain.FromInt(0), "reserved")
	if res.Error != nil {
		t.Fatalf("seed unit %s: %v", code, res.Error)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

// bbSeedAccounts membuat COA minimum untuk BAST (Event 3 + Event 4).
func bbSeedAccounts(t *testing.T, db *gorm.DB) {
	t.Helper()
	accs := []ledger.Account{
		{TenantID: bbTenant, Code: "2-2000", Name: "Uang Muka Penjualan", Type: domain.AccountLiability, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: bbTenant, Code: "1-2000", Name: "Piutang Usaha", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: bbTenant, Code: "4-1000", Name: "Pendapatan Penjualan Unit", Type: domain.AccountRevenue, NormalBalance: domain.NormalBalanceCredit, IsActive: true},
		{TenantID: bbTenant, Code: "5-1000", Name: "HPP", Type: domain.AccountExpense, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: bbTenant, Code: "1-3000", Name: "Persediaan Tanah", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: bbTenant, Code: "1-3100", Name: "Persediaan Hard Cost", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: bbTenant, Code: "1-3200", Name: "Persediaan Soft Cost", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
		{TenantID: bbTenant, Code: "1-3300", Name: "Persediaan Financing", Type: domain.AccountAsset, NormalBalance: domain.NormalBalanceDebit, IsActive: true},
	}
	for i := range accs {
		if err := db.Create(&accs[i]).Error; err != nil {
			t.Fatalf("seed account %s: %v", accs[i].Code, err)
		}
	}
}

// bbApproveRAB membuat + menyetujui satu RAB (project-level) dengan land+hard.
func bbApproveRAB(t *testing.T, svc *budget.Service, projectID uint64, land, hard int64) *budget.BudgetPlan {
	t.Helper()
	ctx := context.Background()
	plan, err := svc.CreatePlan(ctx, bbTenant, budget.CreatePlanRequest{ProjectID: projectID, Label: "RAB"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	add := func(cat budget.BudgetCategory, amt int64) {
		if _, err := svc.AddItem(ctx, bbTenant, budget.AddItemRequest{
			PlanID: plan.ID, Category: cat, BudgetedAmount: domain.FromInt(amt), Description: string(cat),
		}); err != nil {
			t.Fatalf("AddItem %s: %v", cat, err)
		}
	}
	add(budget.BudgetCategoryLand, land)
	add(budget.BudgetCategoryConstruction, hard) // construction → hard
	if _, err := svc.ApprovePlan(ctx, bbTenant, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "owner"}); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	return plan
}

// bbWire merangkai sale.Service produksi dengan resolver budgeted + fallback actual.
func bbWire(db *gorm.DB) (*sale.Service, *sale.GORMRepository, *budget.Service, *allocation.Service) {
	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)
	budgetSvc := budget.NewService(budget.NewGORMRepository(db), budget.NewGORMRealisasiProvider(db))
	resolver := sale.NewBudgetedHPPResolver(budgetSvc, allocSvc, saleRepo)
	svc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo,
		sale.WithHPPResolver(resolver), sale.WithHandoverWriter(saleRepo))
	return svc, saleRepo, budgetSvc, allocSvc
}

// Full path: BAST unit dengan RAB aktif → HPP budgeted, snapshot terpersist,
// jurnal HPP balanced, sale_record ter-link. Plus BCA-1 lintas unit + immutability.
func TestIntegration_BAST_Budgeted_SnapshotAndPosting(t *testing.T) {
	db := itConnect(t)
	bbCleanup(t, db)
	defer bbCleanup(t, db)
	ctx := context.Background()

	project := bbSeedProject(t, db)
	unitA := bbSeedUnit(t, db, project, "A-01", 100) // 25%
	unitB := bbSeedUnit(t, db, project, "B-01", 300) // 75%
	bbSeedAccounts(t, db)

	svc, _, budgetSvc, allocSvc := bbWire(db)
	plan := bbApproveRAB(t, budgetSvc, project, 400_000_000, 800_000_000) // pool land 400jt / hard 800jt
	if err := allocSvc.SetBasis(ctx, bbTenant, project, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis: %v", err)
	}

	// ── BAST unit A (area 100/400 = 25%) ──────────────────────────────────────
	recA, err := svc.RecordAkad(ctx, bbTenant, sale.RecordBASTRequest{
		UnitID: unitA, SalePrice: domain.FromInt(2_000_000_000), BASTDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordBAST A: %v", err)
	}

	// sale_record ter-link ke metode budgeted + versi RAB.
	if recA.HPPMethod != sale.HPPMethodBudgeted {
		t.Errorf("A hpp_method: got %q, want budgeted", recA.HPPMethod)
	}
	if recA.AllocationSnapshotID == nil {
		t.Error("A allocation_snapshot_id harus terisi")
	}
	if recA.BudgetPlanID == nil || *recA.BudgetPlanID != plan.ID {
		t.Errorf("A budget_plan_id: got %v, want %d", recA.BudgetPlanID, plan.ID)
	}
	if recA.BudgetPlanVersion == nil || *recA.BudgetPlanVersion != 1 {
		t.Errorf("A budget_plan_version: got %v, want 1", recA.BudgetPlanVersion)
	}
	// HPP A = 25%: land 100jt, hard 200jt, total 300jt.
	if recA.HPPLand.String() != "100000000" || recA.HPPHard.String() != "200000000" || recA.HPPTotal().String() != "300000000" {
		t.Errorf("A HPP salah: land=%s hard=%s total=%s", recA.HPPLand, recA.HPPHard, recA.HPPTotal())
	}

	// ── Snapshot header + lines ───────────────────────────────────────────────
	var snap sale.AllocationSnapshot
	if err := db.Where("tenant_id = ? AND unit_id = ?", bbTenant, unitA).First(&snap).Error; err != nil {
		t.Fatalf("baca snapshot A: %v", err)
	}
	if snap.Basis != string(allocation.BasisSaleableArea) || snap.HPPTotal.String() != "300000000" {
		t.Errorf("snapshot A header salah: basis=%s total=%s", snap.Basis, snap.HPPTotal)
	}
	if snap.BudgetPlanID != plan.ID || snap.BudgetPlanVersion != 1 {
		t.Errorf("snapshot A RAB ref salah: plan=%d ver=%d", snap.BudgetPlanID, snap.BudgetPlanVersion)
	}
	var lines []sale.AllocationSnapshotLine
	db.Where("tenant_id = ? AND snapshot_id = ?", bbTenant, snap.ID).Find(&lines)
	if len(lines) != 4 {
		t.Fatalf("snapshot A lines: got %d, want 4 (satu per accounting_class)", len(lines))
	}
	byClass := map[string]sale.AllocationSnapshotLine{}
	for _, ln := range lines {
		byClass[ln.AccountingClass] = ln
	}
	if byClass["land"].Amount.String() != "100000000" || byClass["land"].InventoryAccountCode != "1-3000" {
		t.Errorf("line land salah: %+v", byClass["land"])
	}
	if byClass["hard"].Amount.String() != "200000000" || byClass["hard"].InventoryAccountCode != "1-3100" {
		t.Errorf("line hard salah: %+v", byClass["hard"])
	}
	// Bukti basis di tiap baris: area 100 dari total 400 → 25%.
	for _, cls := range []string{"land", "hard", "soft", "financing"} {
		ln := byClass[cls]
		if ln.BasisType != string(allocation.BasisSaleableArea) || ln.BasisValue.String() != "100" || !ln.AllocationPercentage.Equal(decimal.NewFromInt(25)) {
			t.Errorf("bukti basis %s salah: type=%s value=%s pct=%s", cls, ln.BasisType, ln.BasisValue, ln.AllocationPercentage)
		}
	}

	// ── Jurnal HPP (Event 4) balanced: Dr 5-1000 / Cr 1-3xxx ──────────────────
	if recA.COGSJournalID == nil {
		t.Fatal("A COGS journal harus ada (HPP > 0)")
	}
	assertJournalBalanced(t, db, *recA.COGSJournalID, "300000000")

	// ── BAST unit B (75%) → BCA-1 lintas unit ─────────────────────────────────
	recB, err := svc.RecordAkad(ctx, bbTenant, sale.RecordBASTRequest{
		UnitID: unitB, SalePrice: domain.FromInt(3_000_000_000), BASTDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordBAST B: %v", err)
	}
	if recB.HPPLand.String() != "300000000" || recB.HPPHard.String() != "600000000" {
		t.Errorf("B HPP salah: land=%s hard=%s", recB.HPPLand, recB.HPPHard)
	}
	// BCA-1: Σ(lines.amount per class, seluruh unit) == pool per class.
	assertClassSum(t, db, "land", "400000000")
	assertClassSum(t, db, "hard", "800000000")
	assertClassSum(t, db, "soft", "0")
	assertClassSum(t, db, "financing", "0")

	// ── Immutability: supersede RAB (v2) TIDAK mengubah snapshot A ────────────
	bbApproveRAB(t, budgetSvc, project, 999_000_000, 999_000_000) // v2, angka beda
	var snapAfter sale.AllocationSnapshot
	db.Where("tenant_id = ? AND unit_id = ?", bbTenant, unitA).First(&snapAfter)
	if snapAfter.BudgetPlanVersion != 1 || snapAfter.HPPTotal.String() != "300000000" {
		t.Errorf("snapshot A berubah setelah RAB di-supersede: ver=%d total=%s (harus 1/300000000)",
			snapAfter.BudgetPlanVersion, snapAfter.HPPTotal)
	}
}

// Proyek TANPA RAB aktif → metode actual (legacy/backward-compat), tanpa snapshot.
func TestIntegration_BAST_LegacyActual_NoSnapshot(t *testing.T) {
	db := itConnect(t)
	bbCleanup(t, db)
	defer bbCleanup(t, db)
	ctx := context.Background()

	project := bbSeedProject(t, db)
	unit := bbSeedUnit(t, db, project, "L-01", 100)
	bbSeedAccounts(t, db)

	svc, _, _, allocSvc := bbWire(db)
	// Basis diset (untuk jalur actual), TANPA RAB → resolver pilih actual.
	if err := allocSvc.SetBasis(ctx, bbTenant, project, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis: %v", err)
	}

	rec, err := svc.RecordAkad(ctx, bbTenant, sale.RecordBASTRequest{
		UnitID: unit, SalePrice: domain.FromInt(1_000_000_000), BASTDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordBAST legacy: %v", err)
	}
	if rec.HPPMethod != sale.HPPMethodActual {
		t.Errorf("hpp_method: got %q, want actual", rec.HPPMethod)
	}
	if rec.AllocationSnapshotID != nil {
		t.Error("metode actual tidak boleh membuat snapshot")
	}
	var n int64
	db.Table("allocation_snapshots").Where("tenant_id = ?", bbTenant).Count(&n)
	if n != 0 {
		t.Errorf("allocation_snapshots: got %d, want 0 (legacy)", n)
	}
}

func assertJournalBalanced(t *testing.T, db *gorm.DB, journalID uint64, wantDebit string) {
	t.Helper()
	type row struct {
		Debit  domain.Money `gorm:"column:d"`
		Credit domain.Money `gorm:"column:c"`
	}
	var r row
	if err := db.Table("journal_lines").
		Select("COALESCE(SUM(debit),0) AS d, COALESCE(SUM(credit),0) AS c").
		Where("tenant_id = ? AND journal_entry_id = ?", bbTenant, journalID).Scan(&r).Error; err != nil {
		t.Fatalf("sum journal %d: %v", journalID, err)
	}
	if r.Debit.String() != r.Credit.String() {
		t.Errorf("jurnal %d tidak balanced: debit=%s credit=%s", journalID, r.Debit, r.Credit)
	}
	if r.Debit.String() != wantDebit {
		t.Errorf("jurnal %d total debit: got %s, want %s", journalID, r.Debit, wantDebit)
	}
}

func assertClassSum(t *testing.T, db *gorm.DB, class, want string) {
	t.Helper()
	type row struct {
		S domain.Money `gorm:"column:s"`
	}
	var r row
	if err := db.Table("allocation_snapshot_lines").
		Select("COALESCE(SUM(amount),0) AS s").
		Where("tenant_id = ? AND accounting_class = ?", bbTenant, class).Scan(&r).Error; err != nil {
		t.Fatalf("sum class %s: %v", class, err)
	}
	if r.S.String() != want {
		t.Errorf("BCA-1 Σ %s: got %s, want %s (== pool RAB per class)", class, r.S, want)
	}
}

// Increment 6 — integration: Akad menulis satu baris unit_status_transitions DI
// DALAM tx Akad (atomik), event=akad_executed, reference=sale_record, dan status
// unit ter-update ke sold. Membuktikan jalur Akad ikut audit trail lifecycle.
func TestIntegration_BAST_WritesUnitTransitionLog(t *testing.T) {
	db := itConnect(t)
	bbCleanup(t, db)
	defer bbCleanup(t, db)
	ctx := context.Background()

	projectID := bbSeedProject(t, db)
	unitA := bbSeedUnit(t, db, projectID, "A-01", 100)
	bbSeedAccounts(t, db)
	svc, _, budgetSvc, allocSvc := bbWire(db)
	bbApproveRAB(t, budgetSvc, projectID, 400_000_000, 800_000_000)
	if err := allocSvc.SetBasis(ctx, bbTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis: %v", err)
	}

	bastDate := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	rec, err := svc.RecordAkad(ctx, bbTenant, sale.RecordBASTRequest{
		UnitID: unitA, SalePrice: domain.FromInt(2_000_000_000), BASTDate: bastDate,
	})
	if err != nil {
		t.Fatalf("RecordBAST: %v", err)
	}

	var rows []struct {
		FromStatus    string
		ToStatus      string
		Event         string
		ReferenceType string
		ReferenceID   *uint64
		EventDate     time.Time
	}
	if err := db.Raw(`SELECT from_status, to_status, event, reference_type, reference_id, event_date
		FROM unit_status_transitions WHERE tenant_id = ? AND unit_id = ? ORDER BY id`, bbTenant, unitA).
		Scan(&rows).Error; err != nil {
		t.Fatalf("query transitions: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("want exactly 1 transition (BAST), got %d", len(rows))
	}
	r := rows[0]
	if r.FromStatus != "reserved" || r.ToStatus != "sold" {
		t.Errorf("from/to = %s/%s, want reserved/sold", r.FromStatus, r.ToStatus)
	}
	if r.Event != "akad_executed" {
		t.Errorf("event = %s, want akad_executed", r.Event)
	}
	if r.ReferenceType != "sale_record" || r.ReferenceID == nil || *r.ReferenceID != rec.ID {
		t.Errorf("reference = %s/%v, want sale_record/%d", r.ReferenceType, r.ReferenceID, rec.ID)
	}
	// Bandingkan sebagai INSTANT (round-trip loc=Local menggeser wall-clock tapi
	// mempertahankan instant yang sama).
	if !r.EventDate.Equal(bastDate) {
		t.Errorf("event_date instant = %s, want %s", r.EventDate.UTC(), bastDate.UTC())
	}

	var status string
	db.Raw("SELECT status FROM units WHERE id = ?", unitA).Scan(&status)
	if status != "sold" {
		t.Errorf("unit status = %s, want sold", status)
	}
}

// Temuan #7 — integration: RecordPhysicalHandover TIDAK membuat jurnal apa pun
// (murni pencatatan fisik), menulis event terpisah `physically_occupied` di
// unit_status_transitions, memindahkan unit sold→occupied, dan mengisi
// sale_records.handed_over_at — tanpa menyentuh HPP/revenue yang sudah diakui
// saat Akad.
func TestIntegration_RecordPhysicalHandover_NoJournal(t *testing.T) {
	db := itConnect(t)
	bbCleanup(t, db)
	defer bbCleanup(t, db)
	ctx := context.Background()

	projectID := bbSeedProject(t, db)
	unitA := bbSeedUnit(t, db, projectID, "A-01", 100)
	bbSeedAccounts(t, db)
	svc, _, budgetSvc, allocSvc := bbWire(db)
	bbApproveRAB(t, budgetSvc, projectID, 400_000_000, 800_000_000)
	if err := allocSvc.SetBasis(ctx, bbTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis: %v", err)
	}

	akadDate := time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC)
	rec, err := svc.RecordAkad(ctx, bbTenant, sale.RecordBASTRequest{
		UnitID: unitA, SalePrice: domain.FromInt(2_000_000_000), BASTDate: akadDate,
	})
	if err != nil {
		t.Fatalf("RecordAkad: %v", err)
	}

	var journalCountBefore int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", bbTenant).Scan(&journalCountBefore)

	handoverDate := time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC)
	if _, err := svc.RecordPhysicalHandover(ctx, bbTenant, sale.RecordPhysicalHandoverRequest{
		UnitID: unitA, HandoverDate: handoverDate,
	}); err != nil {
		t.Fatalf("RecordPhysicalHandover: %v", err)
	}

	var journalCountAfter int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", bbTenant).Scan(&journalCountAfter)
	if journalCountAfter != journalCountBefore {
		t.Errorf("journal_entries count berubah dari %d ke %d — serah terima fisik TIDAK boleh memposting jurnal",
			journalCountBefore, journalCountAfter)
	}

	var status string
	db.Raw("SELECT status FROM units WHERE id = ?", unitA).Scan(&status)
	if status != "occupied" {
		t.Errorf("unit status = %s, want occupied", status)
	}

	var handedOverAt *time.Time
	db.Raw("SELECT handed_over_at FROM sale_records WHERE id = ?", rec.ID).Scan(&handedOverAt)
	if handedOverAt == nil || !handedOverAt.Equal(handoverDate) {
		t.Errorf("sale_records.handed_over_at = %v, want %s", handedOverAt, handoverDate)
	}

	var rows []struct {
		FromStatus string
		ToStatus   string
		Event      string
	}
	if err := db.Raw(`SELECT from_status, to_status, event FROM unit_status_transitions
		WHERE tenant_id = ? AND unit_id = ? ORDER BY id`, bbTenant, unitA).Scan(&rows).Error; err != nil {
		t.Fatalf("query transitions: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 transitions (akad + handover), got %d: %+v", len(rows), rows)
	}
	h := rows[1]
	if h.FromStatus != "sold" || h.ToStatus != "occupied" {
		t.Errorf("handover from/to = %s/%s, want sold/occupied", h.FromStatus, h.ToStatus)
	}
	if h.Event != "physically_occupied" {
		t.Errorf("handover event = %s, want physically_occupied", h.Event)
	}
}

// Increment 6.1 / F-1 — integration: BAST DITOLAK bila status unit tidak sah
// menurut matriks lifecycle (hanya reserved|ppjb → sold). Tidak ada jurnal,
// snapshot, sale_record, maupun log transisi yang tertulis (rollback penuh).
func TestIntegration_BAST_RejectedWhenUnitNotBASTReady(t *testing.T) {
	db := itConnect(t)
	bbCleanup(t, db)
	defer bbCleanup(t, db)
	ctx := context.Background()

	projectID := bbSeedProject(t, db)
	bbSeedAccounts(t, db)
	svc, _, budgetSvc, allocSvc := bbWire(db)
	bbApproveRAB(t, budgetSvc, projectID, 400_000_000, 800_000_000)
	if err := allocSvc.SetBasis(ctx, bbTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis: %v", err)
	}

	// Katalog produk wajib ada juga di jalur ini (fail-closed): tanpa baris ini
	// BAST gagal karena katalog, sehingga gate status tidak pernah teruji.
	db.Exec(`INSERT IGNORE INTO product_types (tenant_id, code, name, category, revenue_account_code, is_active)
		VALUES (?,?,?,?,?,TRUE)`, bbTenant, "villa", "villa", "property", "4-1000")

	for _, tc := range []struct{ status, code string }{
		{"available", "AV-01"}, // belum pernah reserved
		{"hold", "HD-01"},      // hold administratif — dilindungi gate
		{"blocked", "BL-01"},   // sengketa legal — justru tidak boleh dijual
	} {
		res := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, list_price, status)
			VALUES (?,?,?,?,?,?,?)`,
			bbTenant, projectID, tc.code, "villa", domain.FromInt(100), domain.FromInt(0), tc.status)
		if res.Error != nil {
			t.Fatalf("seed unit %s: %v", tc.code, res.Error)
		}
		var unitID uint64
		db.Raw("SELECT LAST_INSERT_ID()").Scan(&unitID)

		_, err := svc.RecordAkad(ctx, bbTenant, sale.RecordBASTRequest{
			UnitID: unitID, SalePrice: domain.FromInt(1_000_000_000),
			BASTDate: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
		})
		if !errors.Is(err, sale.ErrUnitNotBASTReady) {
			t.Fatalf("BAST dari %s: want ErrUnitNotBASTReady, got %v", tc.status, err)
		}

		// Rollback penuh: tak ada artefak apa pun untuk unit ini.
		for q, label := range map[string]string{
			"SELECT COUNT(*) FROM sale_records WHERE tenant_id=? AND unit_id=?":            "sale_records",
			"SELECT COUNT(*) FROM allocation_snapshots WHERE tenant_id=? AND unit_id=?":    "allocation_snapshots",
			"SELECT COUNT(*) FROM unit_status_transitions WHERE tenant_id=? AND unit_id=?": "unit_status_transitions",
		} {
			var n int64
			db.Raw(q, bbTenant, unitID).Scan(&n)
			if n != 0 {
				t.Errorf("BAST dari %s: %s harus 0, got %d", tc.status, label, n)
			}
		}
		var status string
		db.Raw("SELECT status FROM units WHERE id = ?", unitID).Scan(&status)
		if status != tc.status {
			t.Errorf("status unit %s berubah jadi %s — harus tak tersentuh", tc.status, status)
		}
	}

	// Jurnal global tenant: tidak ada satu pun yang tertulis dari tiga penolakan.
	var journals int64
	db.Raw("SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?", bbTenant).Scan(&journals)
	if journals != 0 {
		t.Errorf("journal_entries harus 0 setelah semua BAST ditolak, got %d", journals)
	}
}
