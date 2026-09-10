//go:build integration

package sale_test

// Product Catalog Hardening (H-1/H-2/M-1) — bukti end-to-end di MySQL nyata.
//
// Yang dibuktikan di sini:
//  1. H-1  Produk non-properti TIDAK ikut basis alokasi HPP. Rumah menerima
//     porsi pool yang sama persis seperti sebelum ada produk non-properti —
//     jurnal identik, bukan "kira-kira sama".
//  2. Non-properti tetap DIJUAL: pendapatannya diakui ke akun mapping-nya
//     (4-2200 test-only, bukan 4-2000 yang kini reserved), tanpa Event 4 (HPP),
//     tanpa snapshot alokasi.
//  3. M-1  Fail-closed: unit_type yang tidak terdaftar menolak BAST dan TIDAK
//     meninggalkan jurnal apa pun (bukan fallback diam-diam ke 4-1000).

import (
	"context"
	"testing"
	"time"

	"gorm.io/gorm"

	"esaproperti/internal/allocation"
	"esaproperti/internal/budget"
	"esaproperti/internal/domain"
	"esaproperti/internal/ledger"
	"esaproperti/internal/project"
	"esaproperti/internal/sale"
)

const pcTenant uint64 = 9_900_058

func pcCleanup(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, tbl := range []string{
		"allocation_snapshot_lines", "allocation_snapshots", "sale_records",
		"journal_lines", "journal_entries", "allocation_executions",
		"allocation_configs", "budget_items", "budget_plans",
		"termin_payments", "unit_status_transitions", "units", "product_types",
		"project_phases", "projects", "accounts",
	} {
		if err := db.Exec("DELETE FROM "+tbl+" WHERE tenant_id = ?", pcTenant).Error; err != nil {
			t.Fatalf("cleanup %s: %v", tbl, err)
		}
	}
}

type pcEnv struct {
	db        *gorm.DB
	svc       *sale.Service
	allocSvc  *allocation.Service
	projectID uint64
}

// pcSeedCatalog mendaftarkan produk. Sengaja lewat repository produksi supaya
// validasi mapping akun (H-2) ikut teruji di jalur fixture.
//
// Akun 4-2200 dibuat KHUSUS untuk test ini (bukan bagian COA produksi) —
// sengaja TIDAK pakai 4-2000: akun itu berperan RoleOtherIncome (Pendapatan
// Luar Usaha) dan sejak ValidateRevenueAccount di-hardening, tidak lagi boleh
// dipetakan ke produk katalog manapun (lihat internal/project/product_type.go).
func pcSeedCatalog(t *testing.T, db *gorm.DB) {
	t.Helper()
	if err := db.Exec(`INSERT INTO accounts (tenant_id, code, name, type, normal_balance, is_active, created_at, updated_at)
		VALUES (?,?,?,?,?,TRUE,NOW(3),NOW(3))`,
		pcTenant, "4-2200", "Pendapatan Produk Tambahan (test)", "revenue", "credit").Error; err != nil {
		t.Fatalf("seed akun 4-2200: %v", err)
	}
	repo := project.NewGORMRepository(db)
	svc := project.NewService(repo, repo, repo)
	svc.SetProductTypeStore(repo)
	ctx := context.Background()
	for _, pt := range []*project.ProductType{
		{Code: "rumah", Name: "Rumah", Category: project.ProductCategoryProperty, RevenueAccountCode: "4-1000"},
		{Code: "pdam", Name: "Sambungan PDAM", Category: project.ProductCategoryNonProperty, RevenueAccountCode: "4-2200"},
	} {
		if _, err := svc.CreateProductType(ctx, pcTenant, pt); err != nil {
			t.Fatalf("seed product type %s: %v", pt.Code, err)
		}
	}
}

// landArea (Item 9, UAT 2026-09-07): units.land_area — fail-closed sejak HPP
// Tanah dialokasikan proporsional terhadap land_area (bukan lagi rata), jadi
// setiap unit PROPERTI yang bisa ikut ComputeBudgeted (lewat RecordAkad) wajib
// diberi nilai > 0 di sini.
func pcSeedUnit(t *testing.T, db *gorm.DB, projectID uint64, code, unitType string, area, landArea, price int64) uint64 {
	t.Helper()
	if err := db.Exec(`INSERT INTO units (tenant_id, project_id, code, unit_type, saleable_area, land_area, list_price, status)
		VALUES (?,?,?,?,?,?,?,?)`,
		pcTenant, projectID, code, unitType, domain.FromInt(area), domain.FromInt(landArea), domain.FromInt(price), "reserved").Error; err != nil {
		t.Fatalf("seed unit %s: %v", code, err)
	}
	var id uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&id)
	return id
}

func pcSetup(t *testing.T, db *gorm.DB) *pcEnv {
	t.Helper()
	ctx := context.Background()
	if err := ledger.SeedCOA(ctx, db, pcTenant); err != nil {
		t.Fatalf("seed COA: %v", err)
	}
	pcSeedCatalog(t, db)

	if err := db.Exec(`INSERT INTO projects (tenant_id, name, status) VALUES (?,?,?)`,
		pcTenant, "Katalog Produk", "selling").Error; err != nil {
		t.Fatalf("seed project: %v", err)
	}
	var projectID uint64
	db.Raw("SELECT LAST_INSERT_ID()").Scan(&projectID)

	ledgerRepo := ledger.NewGORMRepository(db)
	posting := ledger.NewPostingService(ledgerRepo, ledgerRepo).WithPeriodChecker(ledgerRepo)
	allocRepo := allocation.NewGORMRepository(db)
	allocSvc := allocation.NewService(allocRepo, allocRepo, allocRepo)
	saleRepo := sale.NewGORMRepository(db, posting, allocSvc)
	budgetSvc := budget.NewService(budget.NewGORMRepository(db), budget.NewGORMRealisasiProvider(db))
	resolver := sale.NewBudgetedHPPResolver(budgetSvc, allocSvc, saleRepo)
	svc := sale.NewService(saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, saleRepo, sale.WithHPPResolver(resolver))
	// Wiring produksi: kebijakan produk dari master katalog (fail-closed).
	svc.SetProductPolicyResolver(project.NewGORMRepository(db))

	// RAB: pool land 400jt + hard 800jt.
	plan, err := budgetSvc.CreatePlan(ctx, pcTenant, budget.CreatePlanRequest{ProjectID: projectID, Label: "RAB"})
	if err != nil {
		t.Fatalf("CreatePlan: %v", err)
	}
	for _, it := range []struct {
		cat budget.BudgetCategory
		amt int64
	}{
		{budget.BudgetCategoryLand, 400_000_000},
		{budget.BudgetCategoryConstruction, 800_000_000},
	} {
		if _, err := budgetSvc.AddItem(ctx, pcTenant, budget.AddItemRequest{
			PlanID: plan.ID, Category: it.cat, BudgetedAmount: domain.FromInt(it.amt), Description: string(it.cat),
		}); err != nil {
			t.Fatalf("AddItem %s: %v", it.cat, err)
		}
	}
	if _, err := budgetSvc.ApprovePlan(ctx, pcTenant, budget.ApprovePlanRequest{PlanID: plan.ID, ApprovedBy: "owner"}); err != nil {
		t.Fatalf("ApprovePlan: %v", err)
	}
	if err := allocSvc.SetBasis(ctx, pcTenant, projectID, allocation.BasisSaleableArea); err != nil {
		t.Fatalf("SetBasis: %v", err)
	}
	return &pcEnv{db: db, svc: svc, allocSvc: allocSvc, projectID: projectID}
}

// sumJournalLines menjumlahkan debit dan kredit satu akun untuk satu unit.
func pcSumByAccount(t *testing.T, db *gorm.DB, unitID uint64, code string) (debit, credit domain.Money) {
	t.Helper()
	var row struct {
		Debit  domain.Money
		Credit domain.Money
	}
	err := db.Raw(`
		SELECT COALESCE(SUM(jl.debit),0) AS debit, COALESCE(SUM(jl.credit),0) AS credit
		FROM journal_lines jl
		JOIN accounts a ON a.id = jl.account_id
		WHERE jl.tenant_id = ? AND jl.unit_id = ? AND a.code = ?`,
		pcTenant, unitID, code).Scan(&row).Error
	if err != nil {
		t.Fatalf("sum journal lines %s: %v", code, err)
	}
	return row.Debit, row.Credit
}

// H-1: unit non-properti punya area 100 m² — kalau ia ikut DENOMINATOR basis
// Hard (saleable_area), bobot rumah R-01 turun dari 100/400 (25%) menjadi
// 100/500 (20%). Land juga tidak terdilusi oleh non-properti (H-1 mengecualikan
// PDAM-01 dari GetUnitInputs sama sekali) — tapi BUKAN LAGI rata (rule klien
// UAT #1 lama): sejak Item 9 (UAT 2026-09-07), Land proporsional terhadap
// land_area unit. R-01=150 m², R-02=250 m² (total 400) → R-01 dapat 150/400
// = 37.5% dari pool 400jt = 150jt (BUKAN 50%/200jt seperti rule lama).
func TestIntegration_NonProperty_DoesNotDiluteHPP(t *testing.T) {
	db := itConnect(t)
	pcCleanup(t, db)
	defer pcCleanup(t, db)
	ctx := context.Background()
	env := pcSetup(t, db)

	rumahA := pcSeedUnit(t, db, env.projectID, "R-01", "rumah", 100, 150, 500_000_000)
	pcSeedUnit(t, db, env.projectID, "R-02", "rumah", 300, 250, 900_000_000)
	pcSeedUnit(t, db, env.projectID, "PDAM-01", "pdam", 100, 0, 5_000_000) // non-properti

	rec, err := env.svc.RecordAkad(ctx, pcTenant, sale.RecordBASTRequest{
		UnitID: rumahA, SalePrice: domain.FromInt(500_000_000),
		BASTDate: time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordBAST rumah: %v", err)
	}

	// Land: proporsional land_area (Item 9) = 150/400 dari pool 400jt = 150jt
	// (tidak didilusi PDAM-01 — H-1 tetap berlaku, PDAM-01 dikecualikan dari
	// GetUnitInputs sama sekali sehingga tak pernah masuk Σland_area).
	// Hard: 25% dari pool 800jt = 200jt (saleable_area 100/400, PDAM-01
	// dikecualikan dari denominator — H-1).
	wantLand := domain.FromInt(150_000_000)
	wantHard := domain.FromInt(200_000_000)
	if !rec.HPPLand.Equal(wantLand) || !rec.HPPHard.Equal(wantHard) {
		t.Fatalf("HPP rumah = land %s / hard %s, want %s / %s",
			rec.HPPLand, rec.HPPHard, wantLand, wantHard)
	}
	if rec.HPPMethod != "budgeted" {
		t.Errorf("hpp_method = %q, want budgeted", rec.HPPMethod)
	}

	// Bukti audit di snapshot: baris hard/soft/financing = 25% (basis
	// saleable_area, DENOMINATOR hanya unit properti: 100/400, bukan 100/500
	// — H-1), baris land = 37.5% (basis "land_area", 150/400 m² — Item 9).
	var lines []struct {
		AccountingClass string
		BasisType       string
		Pct             string `gorm:"column:allocation_percentage"`
	}
	db.Raw(`SELECT l.accounting_class, l.basis_type, l.allocation_percentage FROM allocation_snapshot_lines l
		JOIN allocation_snapshots s ON s.id = l.snapshot_id
		WHERE s.tenant_id = ? AND s.unit_id = ?`, pcTenant, rumahA).Scan(&lines)
	for _, ln := range lines {
		switch ln.AccountingClass {
		case "land":
			if ln.BasisType != "land_area" || ln.Pct != "37.500000" {
				t.Errorf("baris land: basis_type=%s pct=%s, want land_area/37.500000", ln.BasisType, ln.Pct)
			}
		default:
			if ln.BasisType != "saleable_area" || ln.Pct != "25.000000" {
				t.Errorf("baris %s: basis_type=%s pct=%s, want saleable_area/25.000000 (basis tercemar unit non-properti)",
					ln.AccountingClass, ln.BasisType, ln.Pct)
			}
		}
	}
}

// Non-properti tetap dijual: pendapatan diakui ke akun mapping produk, TANPA
// HPP dan TANPA snapshot alokasi.
func TestIntegration_NonProperty_RevenueWithoutCOGS(t *testing.T) {
	db := itConnect(t)
	pcCleanup(t, db)
	defer pcCleanup(t, db)
	ctx := context.Background()
	env := pcSetup(t, db)

	pcSeedUnit(t, db, env.projectID, "R-01", "rumah", 100, 100, 500_000_000)
	pdam := pcSeedUnit(t, db, env.projectID, "PDAM-01", "pdam", 0, 0, 5_000_000)

	rec, err := env.svc.RecordAkad(ctx, pcTenant, sale.RecordBASTRequest{
		UnitID: pdam, SalePrice: domain.FromInt(5_000_000),
		BASTDate: time.Date(2026, 7, 2, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("RecordBAST non-properti: %v", err)
	}

	// Pendapatan ke akun produk (4-2200 test-only), BUKAN 4-1000.
	if _, cr := pcSumByAccount(t, db, pdam, "4-2200"); !cr.Equal(domain.FromInt(5_000_000)) {
		t.Errorf("kredit 4-2200 = %s, want 5.000.000", cr)
	}
	if _, cr := pcSumByAccount(t, db, pdam, "4-1000"); !cr.IsZero() {
		t.Errorf("kredit 4-1000 = %s, want nol (produk non-properti tidak boleh masuk pendapatan properti)", cr)
	}

	// Tidak ada Event 4: nol debit HPP, nol kredit persediaan.
	if dr, _ := pcSumByAccount(t, db, pdam, "5-1000"); !dr.IsZero() {
		t.Errorf("debit HPP 5-1000 = %s, want nol", dr)
	}
	for _, code := range []string{"1-3000", "1-3100", "1-3200", "1-3300"} {
		if _, cr := pcSumByAccount(t, db, pdam, code); !cr.IsZero() {
			t.Errorf("kredit persediaan %s = %s, want nol", code, cr)
		}
	}

	// sale_record: metode 'none' + HPP nol + tanpa snapshot.
	if rec.HPPMethod != "none" {
		t.Errorf("hpp_method = %q, want none", rec.HPPMethod)
	}
	if !rec.HPPTotal().IsZero() {
		t.Errorf("HPP total = %s, want nol", rec.HPPTotal())
	}
	var snapCount int64
	db.Raw(`SELECT COUNT(*) FROM allocation_snapshots WHERE tenant_id = ? AND unit_id = ?`, pcTenant, pdam).Scan(&snapCount)
	if snapCount != 0 {
		t.Errorf("snapshot alokasi dibuat untuk produk non-properti (%d baris)", snapCount)
	}

	// Jurnal tetap balanced (Invariant #1) — dicek lintas seluruh entry unit ini.
	var diff domain.Money
	db.Raw(`SELECT COALESCE(SUM(debit) - SUM(credit),0) FROM journal_lines
		WHERE tenant_id = ? AND unit_id = ?`, pcTenant, pdam).Scan(&diff)
	if !diff.IsZero() {
		t.Errorf("selisih debit-kredit = %s, want 0", diff)
	}
}

// M-1: unit_type yang tidak ada di katalog menolak BAST tanpa menyisakan jurnal.
func TestIntegration_UnregisteredUnitType_BASTRejected(t *testing.T) {
	db := itConnect(t)
	pcCleanup(t, db)
	defer pcCleanup(t, db)
	ctx := context.Background()
	env := pcSetup(t, db)

	// Unit dengan tipe yang tidak terdaftar (mis. data lama / master dirusak).
	ghost := pcSeedUnit(t, db, env.projectID, "G-01", "hotel", 100, 100, 500_000_000)

	var before int64
	db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, pcTenant).Scan(&before)

	_, err := env.svc.RecordAkad(ctx, pcTenant, sale.RecordBASTRequest{
		UnitID: ghost, SalePrice: domain.FromInt(500_000_000),
		BASTDate: time.Date(2026, 7, 3, 0, 0, 0, 0, time.UTC),
	})
	if err == nil {
		t.Fatal("BAST unit_type tak terdaftar seharusnya ditolak (fail-closed)")
	}

	var after int64
	db.Raw(`SELECT COUNT(*) FROM journal_entries WHERE tenant_id = ?`, pcTenant).Scan(&after)
	if after != before {
		t.Errorf("jurnal bertambah %d padahal BAST ditolak", after-before)
	}
	var status string
	db.Raw(`SELECT status FROM units WHERE id = ?`, ghost).Scan(&status)
	if status == "sold" {
		t.Error("unit berubah menjadi sold padahal BAST ditolak")
	}
}
